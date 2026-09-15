// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// requestLatencyBuckets span 1ms (an in-memory response) to ~16s (a request
// bounded by the p2p timeout).
var requestLatencyBuckets = prometheus.ExponentialBuckets(time.Millisecond.Seconds(), 2, 15)

// Metrics counts one RPC type's client-side requests and their outcomes. The
// base name distinguishes the RPC (e.g. "sync_blocks"), mirroring coreth's
// per-message state sync metrics.
//
// A request is counted exactly once as failed (it never produced a response
// to validate), invalid_response (the response failed validation), or
// succeeded.
type Metrics struct {
	requested       prometheus.Counter
	succeeded       prometheus.Counter
	failed          prometheus.Counter
	invalidResponse prometheus.Counter
	received        prometheus.Counter
	requestLatency  prometheus.Histogram
}

// NewMetrics returns [Metrics] named base_*, registered on reg.
func NewMetrics(reg prometheus.Registerer, base string) (*Metrics, error) {
	m := &Metrics{
		requested: prometheus.NewCounter(prometheus.CounterOpts{
			Name: base + "_requested",
			Help: "Requests sent.",
		}),
		succeeded: prometheus.NewCounter(prometheus.CounterOpts{
			Name: base + "_succeeded",
			Help: "Requests that returned a valid response.",
		}),
		failed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: base + "_failed",
			Help: "Requests that failed before validation, e.g. a network error or timeout.",
		}),
		invalidResponse: prometheus.NewCounter(prometheus.CounterOpts{
			Name: base + "_invalid_response",
			Help: "Responses rejected as invalid, indicating peer misbehavior or corruption.",
		}),
		received: prometheus.NewCounter(prometheus.CounterOpts{
			Name: base + "_received",
			Help: "Items received in valid responses.",
		}),
		requestLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    base + "_request_latency",
			Help:    "Seconds from sending a request until its response arrived.",
			Buckets: requestLatencyBuckets,
		}),
	}
	return m, errors.Join(
		reg.Register(m.requested),
		reg.Register(m.succeeded),
		reg.Register(m.failed),
		reg.Register(m.invalidResponse),
		reg.Register(m.received),
		reg.Register(m.requestLatency),
	)
}
