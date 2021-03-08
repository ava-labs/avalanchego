// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	health "github.com/AppsFlyer/go-sundheit"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Metrics reports commonly used consensus metrics.
type Metrics struct {
	// log reports anomalous events.
	log logging.Logger

	// failingChecks keeps track of the number of check failing
	failingChecks prometheus.Gauge
}

func newMetrics(metricName, descriptionName string, log logging.Logger, namespace string, registerer prometheus.Registerer) (*Metrics, error) {
	metrics := &Metrics{
		log: log,
		failingChecks: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "health_checks_failing",
			Help:      "number of currently failing health checks",
		}),
	}

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(metrics.failingChecks),
	)

	return metrics, errs.Err
}

// healthy handles the metrics for the healthy cases
func (m *Metrics) healthy(json health.Result) {
	m.failingChecks.Dec()
}

// unHealthy handles the metrics for the unhealthy cases
func (m *Metrics) unHealthy(result health.Result) {
	m.failingChecks.Inc()
}
