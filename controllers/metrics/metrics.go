package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	ReconcilesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ngnxop_reconciles_total",
		Help: "Number of total reconcile attempts by nginx operator",
	},
	)
)

func init() {
	metrics.Registry.MustRegister(ReconcilesTotal)
}
