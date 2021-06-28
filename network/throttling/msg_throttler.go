// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ MsgThrottler = &noMsgThrottler{}
	_ MsgThrottler = &sybilMsgThrottler{}
)

// MsgThrottler rate-limits incoming messages from the network.
type MsgThrottler interface {
	// Blocks until node [nodeID] can put a message of
	// size [msgSize] onto the incoming message buffer.
	Acquire(msgSize uint64, nodeID ids.ShortID)

	// Mark that a message from [nodeID] of size [msgSize]
	// has been removed from the incoming message buffer.
	Release(msgSize uint64, nodeID ids.ShortID)
}

// See sybilMsgThrottler
type MsgThrottlerConfig struct {
	MaxVdrBytes         uint64
	MaxAtLargeBytes     uint64
	NodeMaxAtLargeBytes uint64
}

// Returns a new MsgThrottler.
// If this function returns an error, the returned MsgThrottler may still be used.
// However, some of its metrics may not be registered.
func NewSybilMsgThrottler(
	log logging.Logger,
	metricsRegisterer prometheus.Registerer,
	vdrs validators.Set,
	config MsgThrottlerConfig,
) (MsgThrottler, error) {
	t := &sybilMsgThrottler{
		log:                    log,
		cond:                   sync.Cond{L: &sync.Mutex{}},
		vdrs:                   vdrs,
		maxVdrBytes:            config.MaxVdrBytes,
		remainingVdrBytes:      config.MaxVdrBytes,
		remainingAtLargeBytes:  config.MaxAtLargeBytes,
		nodeMaxAtLargeBytes:    config.NodeMaxAtLargeBytes,
		nodeToVdrBytesUsed:     make(map[ids.ShortID]uint64),
		nodeToAtLargeBytesUsed: make(map[ids.ShortID]uint64),
	}
	if err := t.metrics.initialize(metricsRegisterer); err != nil {
		return nil, err
	}
	return t, nil
}

// msgThrottler implements MsgThrottler.
// It gives more space to validators with more stake.
type sybilMsgThrottler struct {
	log     logging.Logger
	metrics sybilMsgThrottlerMetrics
	cond    sync.Cond
	// Primary network validator set
	vdrs validators.Set
	// Max number of unprocessed bytes from validators
	maxVdrBytes uint64
	// Max number of bytes that can be taken from the
	// at-large byte allocation by a given node.
	nodeMaxAtLargeBytes uint64
	// Number of bytes left in the validator byte allocation.
	// Initialized to [maxVdrBytes].
	remainingVdrBytes uint64
	// Number of bytes left in the at-large byte allocation
	remainingAtLargeBytes uint64
	// Node ID --> Bytes they've taken from the validator allocation
	nodeToVdrBytesUsed map[ids.ShortID]uint64
	// Node ID --> Bytes they've taken from the at-large allocation
	nodeToAtLargeBytesUsed map[ids.ShortID]uint64
}

// Returns when we can read a message of size [msgSize] from node [nodeID].
// Release([msgSize], [nodeID]) must be called (!) when done with the message
// or when we give up trying to read the message, if applicable.
func (t *sybilMsgThrottler) Acquire(msgSize uint64, nodeID ids.ShortID) {
	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	t.metrics.awaitingAcquire.Inc()
	startTime := time.Now()

	for { // [t.cond.L] is held while in this loop
		atLargeBytesUsed := t.nodeToAtLargeBytesUsed[nodeID]
		// See if we can take from the at-large byte allocation
		if msgSize <= t.remainingAtLargeBytes && atLargeBytesUsed+msgSize <= t.nodeMaxAtLargeBytes {
			// Take from the at-large byte allocation
			t.remainingAtLargeBytes -= msgSize
			t.nodeToAtLargeBytesUsed[nodeID] += msgSize
			break
		}

		// See if we can use the validator byte allocation
		weight, isVdr := t.vdrs.GetWeight(nodeID)
		if !isVdr {
			// This node isn't a validator.
			// Wait until there are more bytes in an allocation.
			t.cond.Wait()
			continue
		}

		// From the at-large allocation, take all the bytes we can
		// without exceeding the per-node limit on taking from it
		atLargeBytesToUse := t.nodeMaxAtLargeBytes - atLargeBytesUsed
		if atLargeBytesToUse > t.remainingAtLargeBytes {
			atLargeBytesToUse = t.remainingAtLargeBytes
		}
		// Need [vdrBytesNeeded] from the validator allocation.
		vdrBytesNeeded := msgSize - atLargeBytesToUse
		if t.remainingVdrBytes < vdrBytesNeeded {
			// Wait until there are more bytes in an allocation.
			t.cond.Wait()
			continue
		}

		// Number of bytes this node can take from validator allocation.
		vdrBytesAllowed := uint64(0)
		// [totalVdrWeight] should always be > 0 but handle this case
		// for completeness to prevent divide by 0
		totalVdrWeight := t.vdrs.Weight()
		if totalVdrWeight != 0 {
			vdrBytesAllowed = uint64(float64(t.maxVdrBytes) * float64(weight) / float64(totalVdrWeight))
		} else {
			t.log.Warn("total validator weight is 0") // this should never happen
		}
		if t.nodeToVdrBytesUsed[nodeID]+vdrBytesNeeded > vdrBytesAllowed {
			// Wait until there are more bytes in an allocation.
			t.cond.Wait()
			continue
		}

		// Use some of [remainingAtLargeBytes] and some of [remainingVdrBytes]
		t.remainingVdrBytes -= vdrBytesNeeded
		if atLargeBytesToUse != 0 {
			t.nodeToAtLargeBytesUsed[nodeID] += atLargeBytesToUse
		}
		t.remainingAtLargeBytes -= atLargeBytesToUse
		t.nodeToVdrBytesUsed[nodeID] += vdrBytesNeeded
		break
	}
	t.metrics.acquireLatency.Observe(float64(time.Since(startTime)))
	t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
	t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
	t.metrics.awaitingAcquire.Dec()
	t.metrics.awaitingRelease.Inc()
}

func (t *sybilMsgThrottler) Release(msgSize uint64, nodeID ids.ShortID) {
	if msgSize == 0 {
		return // This should never happen
	}
	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	// Try to release these bytes back to the validator allocation
	vdrBytesUsed := t.nodeToVdrBytesUsed[nodeID]
	switch { // This switch is exhaustive
	case vdrBytesUsed > msgSize:
		// Put all bytes back in validator allocation
		t.remainingVdrBytes += msgSize
		t.nodeToVdrBytesUsed[nodeID] -= msgSize
	case vdrBytesUsed == msgSize:
		// Put all bytes back in validator allocation
		t.remainingVdrBytes += msgSize
		delete(t.nodeToVdrBytesUsed, nodeID)
	case vdrBytesUsed < msgSize && vdrBytesUsed > 0:
		// Put some bytes back in validator allocation
		t.remainingVdrBytes += vdrBytesUsed
		t.remainingAtLargeBytes += msgSize - vdrBytesUsed
		t.nodeToAtLargeBytesUsed[nodeID] -= msgSize - vdrBytesUsed
		if t.nodeToAtLargeBytesUsed[nodeID] == 0 {
			delete(t.nodeToAtLargeBytesUsed, nodeID)
		}
		delete(t.nodeToVdrBytesUsed, nodeID)
	case vdrBytesUsed < msgSize && vdrBytesUsed == 0:
		// Put no bytes in validator allocation
		t.remainingAtLargeBytes += msgSize
		t.nodeToAtLargeBytesUsed[nodeID] -= msgSize
		if t.nodeToAtLargeBytesUsed[nodeID] == 0 {
			delete(t.nodeToAtLargeBytesUsed, nodeID)
		}
	}

	t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
	t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
	t.metrics.awaitingRelease.Dec()

	// Notify that there are more bytes available
	t.cond.Broadcast()
}

type sybilMsgThrottlerMetrics struct {
	acquireLatency        prometheus.Histogram
	remainingAtLargeBytes prometheus.Gauge
	remainingVdrBytes     prometheus.Gauge
	awaitingAcquire       prometheus.Gauge
	awaitingRelease       prometheus.Gauge
}

func (m *sybilMsgThrottlerMetrics) initialize(metricsRegisterer prometheus.Registerer) error {
	m.acquireLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_acquire_latency",
		Help:      "Duration an incoming message waited to be read due to throttling",
		Buckets:   metric.NanosecondsBuckets,
	})
	m.remainingAtLargeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_remaining_at_large_bytes",
		Help:      "Bytes remaining in the at large byte allocation",
	})
	m.remainingVdrBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_remaining_validator_bytes",
		Help:      "Bytes remaining in the validator byte allocation",
	})
	m.awaitingAcquire = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_awaiting_acquire",
		Help:      "Number of incoming messages waiting to be read",
	})
	m.awaitingRelease = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_awaiting_release",
		Help:      "Number of messages currently being read/handled",
	})
	errs := wrappers.Errs{}
	errs.Add(
		metricsRegisterer.Register(m.acquireLatency),
		metricsRegisterer.Register(m.remainingAtLargeBytes),
		metricsRegisterer.Register(m.remainingVdrBytes),
		metricsRegisterer.Register(m.awaitingAcquire),
		metricsRegisterer.Register(m.awaitingRelease),
	)
	return errs.Err
}

func NewNoThrottler() MsgThrottler {
	return &noMsgThrottler{}
}

// noMsgThrottler implements MsgThrottler.
// [Acquire] always returns immediately.
type noMsgThrottler struct{}

func (*noMsgThrottler) Acquire(uint64, ids.ShortID) {}

func (*noMsgThrottler) Release(uint64, ids.ShortID) {}
