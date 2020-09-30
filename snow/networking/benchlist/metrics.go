// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	namespace  string
	registerer prometheus.Registerer

	numBenched    prometheus.Gauge
	weightBenched prometheus.Gauge
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(namespace string, registerer prometheus.Registerer) error {
	m.namespace = namespace
	m.registerer = registerer
	errs := wrappers.Errs{}

	m.numBenched = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "benched_num",
		Help:      "Number of currently benched validators",
	})
	if err := registerer.Register(m.numBenched); err != nil {
		errs.Add(fmt.Errorf("failed to register num benched statistics due to %w", err))
	}

	m.weightBenched = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "benched_weight",
		Help:      "Weight of currently benched validators",
	})
	if err := registerer.Register(m.weightBenched); err != nil {
		errs.Add(fmt.Errorf("failed to register weight benched statistics due to %w", err))
	}

	return errs.Err
}
