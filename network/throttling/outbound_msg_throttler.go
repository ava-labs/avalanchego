// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ OutboundMsgThrottler = &outboundMsgThrottler{}
	_ OutboundMsgThrottler = &noOutboundMsgThrottler{}
)

// Rate-limits outgoing messages
type OutboundMsgThrottler interface {
	// Returns true if we can queue a message of size [msgSize] to be sent to node [nodeID].
	// Returns false if the message should be dropped (not sent to [nodeID]).
	// If this method returns true, Release([msgSize], [nodeID]) must be called (!) when
	// the message is sent (or when we give up trying to send the message, if applicable.)
	// If this method returns false, do not make a corresponding call to Release.
	Acquire(msgSize uint64, nodeID ids.ShortID) bool

	// Mark that a message of size [msgSize] has been sent to [nodeID] or we have
	// given up sending the message. Must correspond to a previous call to
	// Acquire([msgSize], [nodeID]) that returned true.
	Release(msgSize uint64, nodeID ids.ShortID)
}

type outboundMsgThrottler struct {
	commonMsgThrottler
	metrics outboundMsgThrottlerMetrics
}

func NewSybilOutboundMsgThrottler(
	log logging.Logger,
	namespace string,
	registerer prometheus.Registerer,
	vdrs validators.Set,
	config MsgByteThrottlerConfig,
) (OutboundMsgThrottler, error) {
	t := &outboundMsgThrottler{
		commonMsgThrottler: commonMsgThrottler{
			log:                    log,
			vdrs:                   vdrs,
			maxVdrBytes:            config.VdrAllocSize,
			remainingVdrBytes:      config.VdrAllocSize,
			remainingAtLargeBytes:  config.AtLargeAllocSize,
			nodeMaxAtLargeBytes:    config.NodeMaxAtLargeBytes,
			nodeToVdrBytesUsed:     make(map[ids.ShortID]uint64),
			nodeToAtLargeBytesUsed: make(map[ids.ShortID]uint64),
		},
	}
	return t, t.metrics.initialize(namespace, registerer)
}

// See OutboundMsgThrottler
func (t *outboundMsgThrottler) Acquire(msgSize uint64, nodeID ids.ShortID) bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Take as many bytes as we can from the at-large allocation.
	bytesNeeded := msgSize
	atLargeBytesUsed := math.Min64(
		// only give as many bytes as needed
		bytesNeeded,
		// don't exceed per-node limit
		t.nodeMaxAtLargeBytes-t.nodeToAtLargeBytesUsed[nodeID],
		// don't give more bytes than are in the allocation
		t.remainingAtLargeBytes,
	)
	bytesNeeded -= atLargeBytesUsed

	// Take as many bytes as we can from [nodeID]'s validator allocation.
	// Calculate [nodeID]'s validator allocation size based on its weight
	vdrAllocationSize := uint64(0)
	weight, isVdr := t.vdrs.GetWeight(nodeID)
	if isVdr && weight != 0 {
		vdrAllocationSize = uint64(float64(t.maxVdrBytes) * float64(weight) / float64(t.vdrs.Weight()))
	}
	vdrBytesAlreadyUsed := t.nodeToVdrBytesUsed[nodeID]
	// [vdrBytesAllowed] is the number of bytes this node
	// may take from its validator allocation.
	vdrBytesAllowed := vdrAllocationSize
	if vdrBytesAlreadyUsed >= vdrAllocationSize {
		// We're already using all the bytes we can from the validator allocation
		vdrBytesAllowed = 0
	} else {
		vdrBytesAllowed -= vdrBytesAlreadyUsed
	}
	vdrBytesUsed := math.Min64(t.remainingVdrBytes, bytesNeeded, vdrBytesAllowed)
	bytesNeeded -= vdrBytesUsed
	if bytesNeeded != 0 {
		// Can't acquire enough bytes to queue this message to be sent
		t.metrics.acquireFailures.Inc()
		return false
	}
	// Can acquire enough bytes to queue this message to be sent.
	// Update the state.
	if atLargeBytesUsed > 0 {
		t.remainingAtLargeBytes -= atLargeBytesUsed
		t.nodeToAtLargeBytesUsed[nodeID] += atLargeBytesUsed
		t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
	}
	if vdrBytesUsed > 0 {
		// Mark that [nodeID] used [vdrBytesUsed] from its validator allocation
		t.remainingVdrBytes -= vdrBytesUsed
		t.nodeToVdrBytesUsed[nodeID] += vdrBytesUsed
		t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
	}
	t.metrics.acquireSuccesses.Inc()
	t.metrics.awaitingRelease.Inc()
	return true
}

// See OutboundMsgThrottler
func (t *outboundMsgThrottler) Release(msgSize uint64, nodeID ids.ShortID) {
	t.lock.Lock()
	defer func() {
		t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
		t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
		t.metrics.awaitingRelease.Dec()
		t.lock.Unlock()
	}()

	// [vdrBytesToReturn] is the number of bytes from [msgSize]
	// that will be given back to [nodeID]'s validator allocation.
	vdrBytesUsed := t.nodeToVdrBytesUsed[nodeID]
	vdrBytesToReturn := math.Min64(msgSize, vdrBytesUsed)
	t.nodeToVdrBytesUsed[nodeID] -= vdrBytesToReturn
	if t.nodeToVdrBytesUsed[nodeID] == 0 {
		delete(t.nodeToVdrBytesUsed, nodeID)
	}
	t.remainingVdrBytes += vdrBytesToReturn

	// [atLargeBytesToReturn] is the number of bytes from [msgSize]
	// that will be given to the at-large allocation.
	atLargeBytesToReturn := msgSize - vdrBytesToReturn
	// Mark that [nodeID] has released these bytes.
	t.remainingAtLargeBytes += atLargeBytesToReturn
	t.nodeToAtLargeBytesUsed[nodeID] -= atLargeBytesToReturn
	if t.nodeToAtLargeBytesUsed[nodeID] == 0 {
		delete(t.nodeToAtLargeBytesUsed, nodeID)
	}
}

type outboundMsgThrottlerMetrics struct {
	acquireSuccesses      prometheus.Counter
	acquireFailures       prometheus.Counter
	remainingAtLargeBytes prometheus.Gauge
	remainingVdrBytes     prometheus.Gauge
	awaitingRelease       prometheus.Gauge
}

func (m *outboundMsgThrottlerMetrics) initialize(namespace string, registerer prometheus.Registerer) error {
	m.acquireSuccesses = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "throttler_outbound_acquire_successes",
		Help:      "Outbound messages not dropped due to rate-limiting",
	})
	m.acquireFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "throttler_outbound_acquire_failures",
		Help:      "Outbound messages dropped due to rate-limiting",
	})
	m.remainingAtLargeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "throttler_outbound_remaining_at_large_bytes",
		Help:      "Bytes remaining in the at large byte allocation",
	})
	m.remainingVdrBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "throttler_outbound_remaining_validator_bytes",
		Help:      "Bytes remaining in the validator byte allocation",
	})
	m.awaitingRelease = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "throttler_outbound_awaiting_release",
		Help:      "Number of messages waiting to be sent",
	})
	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.acquireSuccesses),
		registerer.Register(m.acquireFailures),
		registerer.Register(m.remainingAtLargeBytes),
		registerer.Register(m.remainingVdrBytes),
		registerer.Register(m.awaitingRelease),
	)
	return errs.Err
}

func NewNoOutboundThrottler() OutboundMsgThrottler {
	return &noOutboundMsgThrottler{}
}

// noOutboundMsgThrottler implements OutboundMsgThrottler.
// [Acquire] always returns true. [Release] does nothing.
type noOutboundMsgThrottler struct{}

func (*noOutboundMsgThrottler) Acquire(uint64, ids.ShortID) bool { return true }

func (*noOutboundMsgThrottler) Release(uint64, ids.ShortID) {}
