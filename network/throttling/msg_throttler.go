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
	"github.com/ava-labs/avalanchego/utils/math"
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
	VdrAllocSize        uint64
	AtLargeAllocSize    uint64
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
		cond:                   sync.NewCond(&sync.Mutex{}),
		vdrs:                   vdrs,
		maxVdrBytes:            config.VdrAllocSize,
		remainingVdrBytes:      config.VdrAllocSize,
		remainingAtLargeBytes:  config.AtLargeAllocSize,
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
	cond    *sync.Cond
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
		// Number of bytes this node can take from at-large allocation.
		atLargeBytesAllowed := math.Min64(
			t.nodeMaxAtLargeBytes-t.nodeToAtLargeBytesUsed[nodeID],
			t.remainingAtLargeBytes,
		)

		// Calculate [nodeID]'s validator allocation size based on its weight
		vdrAllocationSize := uint64(0)
		weight, isVdr := t.vdrs.GetWeight(nodeID)
		if isVdr && weight != 0 {
			vdrAllocationSize = uint64(float64(t.maxVdrBytes) * float64(weight) / float64(t.vdrs.Weight()))
		}
		vdrBytesAlreadyUsed := t.nodeToVdrBytesUsed[nodeID]
		// [vdrBytesAllowed] is the number of bytes this node
		// can take from validator allocation.
		vdrBytesAllowed := vdrAllocationSize
		if vdrBytesAlreadyUsed >= vdrAllocationSize {
			// We're using all the bytes we can from the validator allocation
			vdrBytesAllowed = 0
		} else {
			vdrBytesAllowed -= vdrBytesAlreadyUsed
		}

		// Can't acquire enough bytes yet. Wait until more are released.
		if vdrBytesAllowed+atLargeBytesAllowed < msgSize {
			t.cond.Wait()
			continue
		}

		atLargeBytesUsed := math.Min64(msgSize, atLargeBytesAllowed)
		t.remainingAtLargeBytes -= atLargeBytesUsed
		if atLargeBytesUsed != 0 {
			t.nodeToAtLargeBytesUsed[nodeID] += atLargeBytesUsed
		}

		vdrBytesUsed := msgSize - atLargeBytesUsed
		t.remainingVdrBytes -= vdrBytesUsed
		if vdrBytesUsed != 0 {
			t.nodeToVdrBytesUsed[nodeID] += vdrBytesUsed
		}
		break
	}
	t.metrics.acquireLatency.Observe(float64(time.Since(startTime)))
	t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
	t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
	t.metrics.awaitingAcquire.Dec()
	t.metrics.awaitingRelease.Inc()
}

// Must correspond to a previous call of Acquire([msgSize], [nodeID])
func (t *sybilMsgThrottler) Release(msgSize uint64, nodeID ids.ShortID) {
	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	// Release as many bytes as possible back to [nodeID]'s validator allocation
	vdrBytesUsed := t.nodeToVdrBytesUsed[nodeID]
	vdrBytesReturned := math.Min64(msgSize, vdrBytesUsed)
	t.remainingVdrBytes += vdrBytesReturned
	t.nodeToVdrBytesUsed[nodeID] -= vdrBytesReturned
	if t.nodeToVdrBytesUsed[nodeID] == 0 {
		delete(t.nodeToVdrBytesUsed, nodeID)
	}

	// Release the rest of the bytes, if any, back to the at-large allocation
	atLargeBytesReturned := msgSize - vdrBytesReturned
	t.remainingAtLargeBytes += atLargeBytesReturned
	t.nodeToAtLargeBytesUsed[nodeID] -= atLargeBytesReturned
	if t.nodeToAtLargeBytesUsed[nodeID] == 0 {
		delete(t.nodeToAtLargeBytesUsed, nodeID)
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
