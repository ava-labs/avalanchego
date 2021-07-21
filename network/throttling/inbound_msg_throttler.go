// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ InboundMsgThrottler = &noInboundMsgThrottler{}
	_ InboundMsgThrottler = &sybilInboundMsgThrottler{}
)

// InboundMsgThrottler rate-limits incoming messages from the network.
type InboundMsgThrottler interface {
	// Blocks until node [nodeID] can put a message of
	// size [msgSize] onto the incoming message buffer.
	Acquire(msgSize uint64, nodeID ids.ShortID)

	// Mark that a message from [nodeID] of size [msgSize]
	// has been removed from the incoming message buffer.
	Release(msgSize uint64, nodeID ids.ShortID)
}

// Information about a message waiting to be read.
type msgMetadata struct {
	// Need this many more bytes before Acquire returns
	bytesNeeded uint64
	// The sender of this incoming message
	nodeID ids.ShortID
	// Closed when the message can be read.
	closeOnAcquireChan chan struct{}
}

// Returns a new MsgThrottler.
// If this function returns an error, the returned MsgThrottler may still be used.
// However, some of its metrics may not be registered.
func NewSybilInboundMsgThrottler(
	log logging.Logger,
	metricsRegisterer prometheus.Registerer,
	vdrs validators.Set,
	config MsgThrottlerConfig,
) (InboundMsgThrottler, error) {
	t := &sybilInboundMsgThrottler{
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
		waitingToAcquire:    linkedhashmap.New(),
		nodeToWaitingMsgIDs: make(map[ids.ShortID][]uint64),
	}
	if err := t.metrics.initialize(metricsRegisterer); err != nil {
		return nil, err
	}
	return t, nil
}

// msgThrottler implements MsgThrottler.
// It gives more space to validators with more stake.
// Messages are guaranteed to make progress toward
// acquiring enough bytes to be read.
type sybilInboundMsgThrottler struct {
	commonMsgThrottler
	metrics   sybilInboundMsgThrottlerMetrics
	nextMsgID uint64
	// Node ID --> IDs of messages this node is waiting to acquire,
	// order from oldest to most recent.
	nodeToWaitingMsgIDs map[ids.ShortID][]uint64
	// Msg ID --> *msgMetadata
	waitingToAcquire linkedhashmap.LinkedHashmap
	// Invariant: The relative order of messages from a given node
	// are the same in nodeToWaitingMsgIDs[nodeID] and waitingToAcquire.
	// That is, if nodeToAtLargeBytesUsed[nodeID] is [msg0, msg1, msg2]
	// then	waitingToAcquire is [..., msg0, ..., msg1, ..., msg2, ...]
	// where each ... is 0 or more messages.
	//
	// Invariant: waitingToAcquire.Get(nodeToWaitingMsgIDs[nodeID][0])
	// is the info about the message [nodeID] that has been blocking
	// on reading longest
	//
	// Invariant: len(nodeToWaitingMsgIDs[nodeID]) >= 1 for some nodeID
	// implies waitingToAcquire.Len() >= 1, and vice versa.
}

// Returns when we can read a message of size [msgSize] from node [nodeID].
// Release([msgSize], [nodeID]) must be called (!) when done with the message
// or when we give up trying to read the message, if applicable.
func (t *sybilInboundMsgThrottler) Acquire(msgSize uint64, nodeID ids.ShortID) {
	t.metrics.awaitingAcquire.Inc()
	startTime := time.Now()
	defer func() {
		t.metrics.awaitingAcquire.Dec()
		t.metrics.awaitingRelease.Inc()
		t.metrics.acquireLatency.Observe(float64(time.Since(startTime)))
	}()

	bytesNeeded := msgSize
	t.lock.Lock()
	// Take as many bytes as we can from the at-large allocation.
	atLargeBytesUsed := math.Min64(
		// only give as many bytes as needed
		bytesNeeded,
		// don't exceed per-node limit
		t.nodeMaxAtLargeBytes-t.nodeToAtLargeBytesUsed[nodeID],
		// don't give more bytes than are in the allocation
		t.remainingAtLargeBytes,
	)
	if atLargeBytesUsed > 0 {
		t.remainingAtLargeBytes -= atLargeBytesUsed
		t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
		bytesNeeded -= atLargeBytesUsed
		t.nodeToAtLargeBytesUsed[nodeID] += atLargeBytesUsed
		if bytesNeeded == 0 { // If we acquired enough bytes, return
			t.lock.Unlock()
			return
		}
	}

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
	if vdrBytesUsed > 0 {
		// Mark that [nodeID] used [vdrBytesUsed] from its validator allocation
		t.nodeToVdrBytesUsed[nodeID] += vdrBytesUsed
		t.remainingVdrBytes -= vdrBytesUsed
		t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
		bytesNeeded -= vdrBytesUsed
		if bytesNeeded == 0 { // If we acquired enough bytes, return
			t.lock.Unlock()
			return
		}
	}

	// We still haven't acquired enough bytes to read the message.
	// Wait until more bytes are released.

	// [closeOnAcquireChan] is closed when [msgSize] bytes have
	// been acquired and the message can be read.
	closeOnAcquireChan := make(chan struct{})
	t.nextMsgID++
	msgID := t.nextMsgID
	t.waitingToAcquire.Put(
		msgID,
		&msgMetadata{
			bytesNeeded:        bytesNeeded,
			nodeID:             nodeID,
			closeOnAcquireChan: closeOnAcquireChan,
		},
	)
	t.nodeToWaitingMsgIDs[nodeID] = append(t.nodeToWaitingMsgIDs[nodeID], msgID)
	t.lock.Unlock()

	<-closeOnAcquireChan // We've acquired enough bytes
}

// Must correspond to a previous call of Acquire([msgSize], [nodeID])
func (t *sybilInboundMsgThrottler) Release(msgSize uint64, nodeID ids.ShortID) {
	t.lock.Lock()
	defer func() {
		t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
		t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
		t.metrics.awaitingRelease.Dec()
		t.lock.Unlock()
	}()

	// [vdrBytesToReturn] is the number of bytes from [msgSize]
	// that will be given back to [nodeID]'s validator allocation
	// or messages from [nodeID] currently waiting to acquire bytes.
	vdrBytesUsed := t.nodeToVdrBytesUsed[nodeID]
	vdrBytesToReturn := math.Min64(msgSize, vdrBytesUsed)

	// [atLargeBytesToReturn] is the number of bytes from [msgSize]
	// that will be given to the at-large allocation or a message
	// from any node currently waiting to acquire bytes.
	atLargeBytesToReturn := msgSize - vdrBytesToReturn
	if atLargeBytesToReturn > 0 {
		// Mark that [nodeID] has released these bytes.
		t.remainingAtLargeBytes += atLargeBytesToReturn
		t.nodeToAtLargeBytesUsed[nodeID] -= atLargeBytesToReturn
		if t.nodeToAtLargeBytesUsed[nodeID] == 0 {
			delete(t.nodeToAtLargeBytesUsed, nodeID)
		}

		// Iterates over messages waiting to acquire bytes from oldest
		// (waiting the longest) to newest. Try to give bytes to the
		// oldest message, then next oldest, etc. until there are no
		// waiting messages or we exhaust the bytes.
		iter := t.waitingToAcquire.NewIterator()
		for t.remainingAtLargeBytes > 0 && iter.Next() {
			msg := iter.Value().(*msgMetadata)
			// From the at-large allocation, take the maximum number of bytes
			// without exceeding the per-node limit on taking from at-large pool.
			atLargeBytesGiven := math.Min64(
				// don't give [msg] too many bytes
				msg.bytesNeeded,
				// don't exceed per-node limit
				t.nodeMaxAtLargeBytes-t.nodeToAtLargeBytesUsed[msg.nodeID],
				// don't give more bytes than are in the allocation
				t.remainingAtLargeBytes,
			)
			if atLargeBytesGiven > 0 {
				// Mark that we gave [atLargeBytesGiven] to [msg]
				t.nodeToAtLargeBytesUsed[msg.nodeID] += atLargeBytesGiven
				t.remainingAtLargeBytes -= atLargeBytesGiven
				atLargeBytesToReturn -= atLargeBytesGiven
				msg.bytesNeeded -= atLargeBytesGiven
			}
			if msg.bytesNeeded == 0 {
				// [msg] has acquired enough bytes to be read.
				// Unblock the corresponding thread in Acquire
				close(msg.closeOnAcquireChan)
				// Mark that this message is no longer waiting to acquire bytes
				t.nodeToWaitingMsgIDs[msg.nodeID] = t.nodeToWaitingMsgIDs[msg.nodeID][1:]
				if len(t.nodeToWaitingMsgIDs[msg.nodeID]) == 0 {
					delete(t.nodeToWaitingMsgIDs, msg.nodeID)
				}
				t.waitingToAcquire.Delete(iter.Key())
			}
		}
	}

	for vdrBytesToReturn > 0 && len(t.nodeToWaitingMsgIDs[nodeID]) > 0 {
		// Get the next message from [nodeID] waiting to acquire
		msgID := t.nodeToWaitingMsgIDs[nodeID][0]
		msgIntf, exists := t.waitingToAcquire.Get(msgID)
		if !exists {
			// This should never happen
			t.log.Warn("couldn't find message %s from %s%s", msgID, constants.NodeIDPrefix, nodeID)
			break
		}
		// Give [msg] all the bytes we can
		msg := msgIntf.(*msgMetadata)
		bytesToGive := math.Min64(msg.bytesNeeded, vdrBytesToReturn)
		msg.bytesNeeded -= bytesToGive
		vdrBytesToReturn -= bytesToGive
		if msg.bytesNeeded == 0 {
			// Unblock the corresponding thread in Acquire
			close(msg.closeOnAcquireChan)
			// Mark that this message is no longer waiting to acquire bytes
			t.nodeToWaitingMsgIDs[nodeID] = t.nodeToWaitingMsgIDs[nodeID][1:]
			if len(t.nodeToWaitingMsgIDs[nodeID]) == 0 {
				delete(t.nodeToWaitingMsgIDs, nodeID)
			}
			t.waitingToAcquire.Delete(msgID)
		}
	}
	if vdrBytesToReturn > 0 {
		// We gave back all the bytes we could to waiting messages from [nodeID]
		// but some are still left.
		t.nodeToVdrBytesUsed[nodeID] -= vdrBytesToReturn
		if t.nodeToVdrBytesUsed[nodeID] == 0 {
			delete(t.nodeToVdrBytesUsed, nodeID)
		}
		t.remainingVdrBytes += vdrBytesToReturn
	}
}

type sybilInboundMsgThrottlerMetrics struct {
	acquireLatency        metric.Averager
	remainingAtLargeBytes prometheus.Gauge
	remainingVdrBytes     prometheus.Gauge
	awaitingAcquire       prometheus.Gauge
	awaitingRelease       prometheus.Gauge
}

func (m *sybilInboundMsgThrottlerMetrics) initialize(reg prometheus.Registerer) error {
	errs := wrappers.Errs{}
	m.acquireLatency = metric.NewAveragerWithErrs(
		constants.PlatformName,
		"throttler_inbound_acquire_latency",
		"time (in ns) of an incoming message waiting to be read due to throttling",
		reg,
		&errs,
	)
	m.remainingAtLargeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_inbound_remaining_at_large_bytes",
		Help:      "Bytes remaining in the at large byte allocation",
	})
	m.remainingVdrBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_inbound_remaining_validator_bytes",
		Help:      "Bytes remaining in the validator byte allocation",
	})
	m.awaitingAcquire = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_inbound_awaiting_acquire",
		Help:      "Number of incoming messages waiting to be read",
	})
	m.awaitingRelease = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.PlatformName,
		Name:      "throttler_inbound_awaiting_release",
		Help:      "Number of messages currently being read/handled",
	})
	errs.Add(
		reg.Register(m.remainingAtLargeBytes),
		reg.Register(m.remainingVdrBytes),
		reg.Register(m.awaitingAcquire),
		reg.Register(m.awaitingRelease),
	)
	return errs.Err
}

func NewNoInboundThrottler() InboundMsgThrottler {
	return &noInboundMsgThrottler{}
}

// noMsgThrottler implements MsgThrottler.
// [Acquire] always returns immediately.
type noInboundMsgThrottler struct{}

func (*noInboundMsgThrottler) Acquire(uint64, ids.ShortID) {}

func (*noInboundMsgThrottler) Release(uint64, ids.ShortID) {}
