// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package sync syncs a merkle trie over the p2p network: a [Syncer] (client)
// requests range and change proofs that a [ProofHandler] (server) generates.
//
// Metrics are split by component: [syncerMetrics] is updated only by [Syncer]
// and [handlerMetrics] only by [ProofHandler], so each component registers
// only the collectors it updates.
package sync

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	proofTypeLabel  = "proof_type"
	proofTypeRange  = "range"
	proofTypeChange = "change"

	resultLabel   = "result"
	resultSuccess = "success"
	resultFailure = "failure"
)

// resultLabelFor returns the result label value for an operation that
// returned err.
func resultLabelFor(err error) string {
	if err != nil {
		return resultFailure
	}
	return resultSuccess
}

var (
	// durationBuckets span 1ms (fast proof operation) to ~8.2s (slow
	// generation, verification, or commit).
	durationBuckets = prometheus.ExponentialBuckets(0.001, 2, 14)
	// proofSizeBuckets span 1 KiB (near-empty proof) to 2 MiB (proof at
	// [maxByteSizeLimit]).
	proofSizeBuckets = prometheus.ExponentialBuckets(1024, 2, 12)
	// keyLimitBuckets span 1 (maximally shrunk) to 2048 ([MaxKeyValuesLimit]).
	keyLimitBuckets = prometheus.ExponentialBuckets(1, 2, 12)
)

// syncerMetrics observes the client side of proof sync: requesting,
// receiving, verifying, and committing proofs in [Syncer].
type syncerMetrics struct {
	requestsFailed         prometheus.Counter
	requestsMade           prometheus.Counter
	requestsSucceeded      prometheus.Counter
	requestKeyLimit        prometheus.Gauge
	receivedProofSizeBytes *prometheus.HistogramVec
	proofVerificationTime  *prometheus.HistogramVec
	proofCommitTime        *prometheus.HistogramVec
}

func newSyncerMetrics(namespace string, reg prometheus.Registerer) (*syncerMetrics, error) {
	m := syncerMetrics{
		requestsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_failed",
			Help:      "cumulative amount of failed proof requests",
		}),
		requestsMade: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_made",
			Help:      "cumulative amount of proof requests made",
		}),
		requestsSucceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_succeeded",
			Help:      "cumulative amount of proof requests that were successful",
		}),
		requestKeyLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "request_key_limit",
			Help:      "maximum number of key/value pairs requested per proof request",
		}),
		receivedProofSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "received_proof_size_bytes",
			Help:      "size, in bytes, of each received proof response",
			Buckets:   proofSizeBuckets,
		}, []string{proofTypeLabel}),
		proofVerificationTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "proof_verification_seconds",
			Help:      "time, in seconds, spent verifying each received proof, labeled by result",
			Buckets:   durationBuckets,
		}, []string{proofTypeLabel, resultLabel}),
		proofCommitTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "proof_commit_seconds",
			Help:      "time, in seconds, spent committing each verified proof to the database, labeled by result",
			Buckets:   durationBuckets,
		}, []string{proofTypeLabel, resultLabel}),
	}
	m.requestKeyLimit.Set(DefaultRequestKeyLimit)
	err := errors.Join(
		reg.Register(m.requestsFailed),
		reg.Register(m.requestsMade),
		reg.Register(m.requestsSucceeded),
		reg.Register(m.requestKeyLimit),
		reg.Register(m.receivedProofSizeBytes),
		reg.Register(m.proofVerificationTime),
		reg.Register(m.proofCommitTime),
	)
	return &m, err
}

func (m *syncerMetrics) requestFailed() {
	m.requestsFailed.Inc()
}

func (m *syncerMetrics) requestMade() {
	m.requestsMade.Inc()
}

func (m *syncerMetrics) requestSucceeded() {
	m.requestsSucceeded.Inc()
}

func (m *syncerMetrics) proofReceived(proofType string, numBytes int) {
	m.receivedProofSizeBytes.WithLabelValues(proofType).Observe(float64(numBytes))
}

func (m *syncerMetrics) observeVerification(proofType string, duration time.Duration, err error) {
	m.proofVerificationTime.WithLabelValues(proofType, resultLabelFor(err)).Observe(duration.Seconds())
}

func (m *syncerMetrics) observeCommit(proofType string, duration time.Duration, err error) {
	m.proofCommitTime.WithLabelValues(proofType, resultLabelFor(err)).Observe(duration.Seconds())
}

// handlerMetrics observes the server side of proof sync: generating,
// shrinking, and serving proofs in [ProofHandler].
type handlerMetrics struct {
	proofGenerationTime     *prometheus.HistogramVec
	generatedProofSizeBytes *prometheus.HistogramVec
	proofShrinkNewKeyLimit  *prometheus.HistogramVec
}

func newHandlerMetrics(namespace string, reg prometheus.Registerer) (*handlerMetrics, error) {
	m := handlerMetrics{
		proofGenerationTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "proof_generation_seconds",
			Help:      "time, in seconds, spent generating each proof, labeled by result; the count is the total number of generation attempts",
			Buckets:   durationBuckets,
		}, []string{proofTypeLabel, resultLabel}),
		generatedProofSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "generated_proof_size_bytes",
			Help:      "size, in bytes, of each marshaled proof served to a peer",
			Buckets:   proofSizeBuckets,
		}, []string{proofTypeLabel}),
		proofShrinkNewKeyLimit: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "proof_shrink_new_key_limit",
			Help:      "key limit after halving because a generated proof exceeded the byte limit; the count is the total number of shrink events",
			Buckets:   keyLimitBuckets,
		}, []string{proofTypeLabel}),
	}
	err := errors.Join(
		reg.Register(m.proofGenerationTime),
		reg.Register(m.generatedProofSizeBytes),
		reg.Register(m.proofShrinkNewKeyLimit),
	)
	return &m, err
}

// observeGeneration records one proof generation attempt, labeling it a
// failure if err is non-nil. A failure includes a change proof attempt that
// falls back to a range proof; the fallback records its own range attempt.
func (m *handlerMetrics) observeGeneration(proofType string, duration time.Duration, err error) {
	m.proofGenerationTime.WithLabelValues(proofType, resultLabelFor(err)).Observe(duration.Seconds())
}

func (m *handlerMetrics) proofServed(proofType string, numBytes int) {
	m.generatedProofSizeBytes.WithLabelValues(proofType).Observe(float64(numBytes))
}

func (m *handlerMetrics) proofShrunk(proofType string, newKeyLimit int) {
	m.proofShrinkNewKeyLimit.WithLabelValues(proofType).Observe(float64(newKeyLimit))
}
