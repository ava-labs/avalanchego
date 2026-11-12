// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"errors"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

const metricsNamespace = "sync"

type metrics struct {
	requestsMade      prometheus.Counter
	requestsSucceeded prometheus.Counter
	requestsFailed    prometheus.Counter
	proofsAccepted    prometheus.Counter
	proofsRejected    prometheus.Counter
	proofBytes        prometheus.Counter
	// submitSeconds is the time Firewood spends verifying and committing a
	// proof, measured from receipt to verdict.
	submitSeconds prometheus.Histogram

	// Mirrors of the counters above for progress logging.
	accepted      atomic.Uint64
	acceptedBytes atomic.Uint64
	rejected      atomic.Uint64
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	counter := func(name, help string) prometheus.Counter {
		return prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      name,
			Help:      help,
		})
	}
	m := &metrics{
		requestsMade:      counter("requests_made", "number of range proof requests sent"),
		requestsSucceeded: counter("requests_succeeded", "number of well-formed range proof responses"),
		requestsFailed:    counter("requests_failed", "number of failed or malformed range proof responses"),
		proofsAccepted:    counter("proofs_accepted", "number of range proofs firewood verified and committed"),
		proofsRejected:    counter("proofs_rejected", "number of range proofs firewood rejected"),
		proofBytes:        counter("accepted_proof_bytes", "total bytes of range proofs firewood committed"),
		submitSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "proof_submit_seconds",
			Help:      "time firewood takes to verify and commit a range proof",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15),
		}),
	}
	return m, errors.Join(
		reg.Register(m.requestsMade),
		reg.Register(m.requestsSucceeded),
		reg.Register(m.requestsFailed),
		reg.Register(m.proofsAccepted),
		reg.Register(m.proofsRejected),
		reg.Register(m.proofBytes),
		reg.Register(m.submitSeconds),
	)
}
