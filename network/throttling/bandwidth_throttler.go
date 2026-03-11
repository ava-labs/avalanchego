// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var _ bandwidthThrottler = (*bandwidthThrottlerImpl)(nil)

// Returns a bandwidth throttler that uses a token bucket
// model, where each token is 1 byte, to rate-limit bandwidth usage.
// See https://pkg.go.dev/golang.org/x/time/rate#Limiter
type bandwidthThrottler interface {
	// Blocks until [nodeID] can read a message of size [msgSize].
	// AddNode([nodeID], ...) must have been called since
	// the last time RemoveNode([nodeID]) was called, if any.
	// It's safe for multiple goroutines to concurrently call Acquire.
	// Returns immediately if [ctx] is canceled.
	Acquire(ctx context.Context, msgSize uint64, nodeID ids.NodeID)

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

func newBandwidthThrottler(
	log logging.Logger,
	registerer prometheus.Registerer,
	config BandwidthThrottlerConfig,
) (bandwidthThrottler, error) {
	errs := wrappers.Errs{}
	t := &bandwidthThrottlerImpl{
		BandwidthThrottlerConfig: config,
		log:                      log,
		limiters:                 make(map[ids.NodeID]*rate.Limiter),
		metrics: bandwidthThrottlerMetrics{
			acquireLatency: metric.NewAveragerWithErrs(
				"bandwidth_throttler_inbound_acquire_latency",
				"average time (in ns) to acquire bytes from the inbound bandwidth throttler",
				registerer,
				&errs,
			),
			awaitingAcquire: prometheus.NewGauge(prometheus.GaugeOpts{
				Name: "bandwidth_throttler_inbound_awaiting_acquire",
				Help: "Number of inbound messages waiting to acquire bandwidth from the inbound bandwidth throttler",
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

type bandwidthThrottlerImpl struct {
	BandwidthThrottlerConfig

	metrics bandwidthThrottlerMetrics
	log     logging.Logger
	lock    sync.RWMutex
	// Node ID --> token bucket based rate limiter where each token
	// is a byte of bandwidth.
	limiters map[ids.NodeID]*rate.Limiter
}

// See BandwidthThrottler.
func (t *bandwidthThrottlerImpl) Acquire(
	ctx context.Context,
	msgSize uint64,
	nodeID ids.NodeID,
) {
	startTime := time.Now()
	t.metrics.awaitingAcquire.Inc()
	defer func() {
		t.metrics.acquireLatency.Observe(float64(time.Since(startTime)))
		t.metrics.awaitingAcquire.Dec()
	}()

	t.lock.RLock()
	limiter, ok := t.limiters[nodeID]
	t.lock.RUnlock()
	if !ok {
		// This should never happen. If it is, the caller is misusing this struct.
		t.log.Debug("tried to acquire throttler but the node isn't registered",
			zap.Uint64("messageSize", msgSize),
			zap.Stringer("nodeID", nodeID),
		)
		return
	}
	if err := limiter.WaitN(ctx, int(msgSize)); err != nil {
		// This should only happen on shutdown.
		t.log.Debug("error while waiting for throttler",
			zap.Uint64("messageSize", msgSize),
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
	}
}

// See BandwidthThrottler.
func (t *bandwidthThrottlerImpl) AddNode(nodeID ids.NodeID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.limiters[nodeID]; ok {
		t.log.Debug("tried to add peer but it's already registered",
			zap.Stringer("nodeID", nodeID),
		)
		return
	}
	t.limiters[nodeID] = rate.NewLimiter(rate.Limit(t.RefillRate), int(t.MaxBurstSize))
}

// See BandwidthThrottler.
func (t *bandwidthThrottlerImpl) RemoveNode(nodeID ids.NodeID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.limiters[nodeID]; !ok {
		t.log.Debug("tried to remove peer but it isn't registered",
			zap.Stringer("nodeID", nodeID),
		)
		return
	}
	delete(t.limiters, nodeID)
}
