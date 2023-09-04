/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"github.com/shanmugara/nginx-operator/controllers/metrics"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/shanmugara/nginx-operator/api/v1alpha1"
	"github.com/shanmugara/nginx-operator/assets"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

const (
	OperatorDegraded                   = "OperatorDegraded"
	ReasonCRNotAvailable               = "OperatorResourceNotAvailable"
	ReasonOperandDeploymentUnavailable = "OperandDeploymentUnavailable"
	ReasonOperandDeploymentFailed      = "OperandDeploymentFailed"
	ReasonOperatorSucceeded            = "OperatorSucceeded"
)

var (
	RequeShort, _ = time.ParseDuration("5s")
)

// NginxOperatorReconciler reconciles a NginxOperator object
type NginxOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=operator.omegahome.net,resources=nginxoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.omegahome.net,resources=nginxoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.omegahome.net,resources=nginxoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NginxOperator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *NginxOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.ReconcilesTotal.Inc()
	logger := log.FromContext(ctx)
	operatorCR := &operatorv1alpha1.NginxOperator{}

	logger.Info("namespacedname", "namespacedname", req.NamespacedName)

	err := r.Get(ctx, req.NamespacedName, operatorCR)

	if err != nil && errors.IsNotFound(err) {
		logger.Info("Operator resource object not found")
		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error(err, "Error getting operator resource object")
		meta.SetStatusCondition(&operatorCR.Status.Conditions, metav1.Condition{
			Type:               OperatorDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonCRNotAvailable,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Message:            fmt.Sprintf("unable to get operator custon resource: %s", err.Error()),
		})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, operatorCR)})
	}

	//// OLM OperatorCondition
	//condition, err := conditions.InClusterFactory{Client: r.Client}.NewCondition(apiv2.ConditionType(apiv2.Upgradeable))
	//if err != nil {
	//	return ctrl.Result{}, err
	//}
	//err = condition.Set(ctx, metav1.ConditionTrue, conditions.WithReason("OperatorUpgradeable"),
	//	conditions.WithMessage("The operator is upgradeable"))
	//if err != nil {
	//	return ctrl.Result{}, err
	//}
	//// OLM end

	deployment := &appsv1.Deployment{}
	create := false

	err = r.Get(ctx, req.NamespacedName, deployment)

	if err != nil && errors.IsNotFound(err) {
		create = true
		logger.Info("Deployment not found")
		deployment = assets.GetDeploymentFromFile("manifests/nginx_deployment.yaml")

	} else if err != nil {
		logger.Error(err, "Error getting existing nginx deployment")
		meta.SetStatusCondition(&operatorCR.Status.Conditions, metav1.Condition{
			Type:               OperatorDegraded,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Reason:             ReasonOperandDeploymentUnavailable,
			Message:            fmt.Sprintf("unable to get operand deploymnet: %s", err.Error()),
		})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, operatorCR)})
	}

	deployment.Namespace = req.Namespace
	deployment.Name = req.Name

	if operatorCR.Spec.Replicas != nil {
		deployment.Spec.Replicas = operatorCR.Spec.Replicas
	}
	if operatorCR.Spec.Port != nil {
		deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort = *operatorCR.Spec.Port
	}

	err = ctrl.SetControllerReference(operatorCR, deployment, r.Scheme)

	if err != nil {
		return ctrl.Result{}, err
	}

	if create {
		logger.Info("creating missing deployment")
		err = r.Create(ctx, deployment)
	} else {
		logger.Info("updating misconfigured deployment")
		err = r.Update(ctx, deployment)
	}

	if err != nil {
		logger.Info("debug log for error", "err", err.Error())
		meta.SetStatusCondition(&operatorCR.Status.Conditions, metav1.Condition{
			Type:               OperatorDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonOperandDeploymentFailed,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Message:            fmt.Sprintf("unable to update deployment: %s", err.Error()),
		})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, operatorCR)})
	}

	meta.SetStatusCondition(&operatorCR.Status.Conditions, metav1.Condition{
		Type:               OperatorDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonOperatorSucceeded,
		LastTransitionTime: metav1.NewTime(time.Now()),
		Message:            "operator successfully reconciling",
	})

	return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, operatorCR)})
}

// SetupWithManager sets up the controller with the Manager.
func (r *NginxOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.NginxOperator{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
