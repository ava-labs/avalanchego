// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

// Polls reports commonly used consensus poll metrics.
type Polls struct {
	// numFailedPolls keeps track of the number of polls that failed
	numFailedPolls prometheus.Counter

	// numSuccessfulPolls keeps track of the number of polls that succeeded
	numSuccessfulPolls prometheus.Counter
}

// Initialize the metrics.
func (m *Polls) Initialize(namespace string, reg prometheus.Registerer) error {
	m.numFailedPolls = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "polls_failed",
		Help:      "Number of failed polls",
	})

	m.numSuccessfulPolls = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "polls_successful",
		Help:      "Number of successful polls",
	})

	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.numFailedPolls),
		reg.Register(m.numSuccessfulPolls),
	)
	return errs.Err
}

func (m *Polls) Failed() {
	m.numFailedPolls.Inc()
}

func (m *Polls) Successful() {
	m.numSuccessfulPolls.Inc()
}
