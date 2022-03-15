// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

var _ BandwidthThrottler = &bandwidthThrottler{}

// Returns a bandwidth throttler that uses a token bucket
// model, where each token is 1 byte, to rate-limit bandwidth usage.
// See https://pkg.go.dev/golang.org/x/time/rate#Limiter
type BandwidthThrottler interface {
	// Blocks until [nodeID] can read a message of size [msgSize].
	// AddNode([nodeID], ...) must have been called since
	// the last time RemoveNode([nodeID]) was called, if any.
	// It's safe for multiple goroutines to concurrently call Acquire.
	Acquire(msgSize uint64, nodeID ids.NodeID)

	// Add a new node to this throttler.
	// Must be called before Acquire(..., [nodeID]) is called.
	// RemoveNode([nodeID]) must have been called since the last time
	// AddNode([nodeID], ...) was called, if any.
	// Its bandwidth allocation refills at a rate of [refillRate].
	// Its bandwidth allocation can hold up to [maxBurstSize] at a time.
	// [maxBurstSize] must be at least the maximum message size.
	// It's safe for multiple goroutines to concurrently call AddNode.
	AddNode(nodeID ids.NodeID)

	// Remove a node from this throttler.
	// AddNode([nodeID], ...) must have been called since
	// the last time RemoveNode([nodeID]) was called, if any.
	// Must be called when we stop reading messages from [nodeID].
	// It's safe for multiple goroutines to concurrently call RemoveNode.
	RemoveNode(nodeID ids.NodeID)
}

type BandwidthThrottlerConfig struct {
	// Rate at which the inbound bandwidth consumable by a peer replenishes
	RefillRate uint64 `json:"bandwidthRefillRate"`
	// Max amount of consumable bandwidth that can accumulate for a given peer
	MaxBurstSize uint64 `json:"bandwidthMaxBurstRate"`
}

func NewBandwidthThrottler(
	log logging.Logger,
	namespace string,
	registerer prometheus.Registerer,
	config BandwidthThrottlerConfig,
) (BandwidthThrottler, error) {
	errs := wrappers.Errs{}
	t := &bandwidthThrottler{
		BandwidthThrottlerConfig: config,
		log:                      log,
		limiters:                 make(map[ids.NodeID]*rate.Limiter),
		metrics: bandwidthThrottlerMetrics{
			acquireLatency: metric.NewAveragerWithErrs(
				namespace,
				"bandwidth_throttler_inbound_acquire_latency",
				"average time (in ns) to acquire bytes from the inbound bandwidth throttler",
				registerer,
				&errs,
			),
			awaitingAcquire: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "bandwidth_throttler_inbound_awaiting_acquire",
				Help:      "Number of inbound messages waiting to acquire bandwidth from the inbound bandwidth throttler",
			}),
		},
	}
	errs.Add(registerer.Register(t.metrics.awaitingAcquire))
	return t, errs.Err
}

type bandwidthThrottlerMetrics struct {
	acquireLatency  metric.Averager
	awaitingAcquire prometheus.Gauge
}

type bandwidthThrottler struct {
	BandwidthThrottlerConfig
	metrics bandwidthThrottlerMetrics
	log     logging.Logger
	lock    sync.RWMutex
	// Node ID --> token bucket based rate limiter where each token
	// is a byte of bandwidth.
	limiters map[ids.NodeID]*rate.Limiter
}

// See BandwidthThrottler.
func (t *bandwidthThrottler) Acquire(msgSize uint64, nodeID ids.NodeID) {
	startTime := time.Now()
	defer func() {
		t.metrics.acquireLatency.Observe(float64(time.Since(startTime)))
		t.metrics.awaitingAcquire.Dec()
	}()

	t.metrics.awaitingAcquire.Inc()
	t.lock.RLock()
	limiter, ok := t.limiters[nodeID]
	t.lock.RUnlock()
	if !ok {
		// This should never happen. If it is, the caller is misusing this struct.
		t.log.Debug("tried to acquire %d bytes for %s but that node isn't registered", msgSize, nodeID)
		return
	}
	// TODO Allow cancellation using context?
	if err := limiter.WaitN(context.TODO(), int(msgSize)); err != nil {
		// This should never happen.
		t.log.Warn("error while awaiting %d bytes for %s: %s", msgSize, nodeID, err)
	}
}

// See BandwidthThrottler.
func (t *bandwidthThrottler) AddNode(nodeID ids.NodeID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.limiters[nodeID]; ok {
		t.log.Debug("tried to add %s but it's already registered", nodeID)
	}
	t.limiters[nodeID] = rate.NewLimiter(rate.Limit(t.RefillRate), int(t.MaxBurstSize))
}

// See BandwidthThrottler.
func (t *bandwidthThrottler) RemoveNode(nodeID ids.NodeID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.limiters[nodeID]; !ok {
		t.log.Debug("tried to remove %s but it isn't registered", nodeID)
	}
	delete(t.limiters, nodeID)
}
