// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// See inbound_msg_throttler.go

func newInboundMsgByteThrottler(
	log logging.Logger,
	registerer prometheus.Registerer,
	vdrs validators.Manager,
	config MsgByteThrottlerConfig,
) (*inboundMsgByteThrottler, error) {
	t := &inboundMsgByteThrottler{
		commonMsgThrottler: commonMsgThrottler{
			log:                    log,
			vdrs:                   vdrs,
			maxVdrBytes:            config.VdrAllocSize,
			remainingVdrBytes:      config.VdrAllocSize,
			remainingAtLargeBytes:  config.AtLargeAllocSize,
			nodeMaxAtLargeBytes:    config.NodeMaxAtLargeBytes,
			nodeToVdrBytesUsed:     make(map[ids.NodeID]uint64),
			nodeToAtLargeBytesUsed: make(map[ids.NodeID]uint64),
		},
		waitingToAcquire:   linked.NewHashmap[uint64, *msgMetadata](),
		nodeToWaitingMsgID: make(map[ids.NodeID]uint64),
	}
	return t, t.metrics.initialize(registerer)
}

// Information about a message waiting to be read.
type msgMetadata struct {
	// Need this many more bytes before Acquire returns
	bytesNeeded uint64
	// The number of bytes that were attempted to be acquired
	msgSize uint64
	// The sender of this incoming message
	nodeID ids.NodeID
	// Closed when the message can be read.
	closeOnAcquireChan chan struct{}
}

// It gives more space to validators with more stake.
// Messages are guaranteed to make progress toward
// acquiring enough bytes to be read.
type inboundMsgByteThrottler struct {
	commonMsgThrottler
	metrics   inboundMsgByteThrottlerMetrics
	nextMsgID uint64
	// Node ID --> Msg ID for a message this node is waiting to acquire
	nodeToWaitingMsgID map[ids.NodeID]uint64
	// Msg ID --> *msgMetadata
	waitingToAcquire *linked.Hashmap[uint64, *msgMetadata]
	// Invariant: The node is only waiting on a single message at a time
	//
	// Invariant: waitingToAcquire.Get(nodeToWaitingMsgIDs[nodeID])
	// is the info about the message [nodeID] that has been blocking
	// on reading.
	//
	// Invariant: len(nodeToWaitingMsgIDs) >= 1
	// implies waitingToAcquire.Len() >= 1, and vice versa.
}

// Returns when we can read a message of size [msgSize] from node [nodeID].
// The returned ReleaseFunc must be called (!) when done with the message
// or when we give up trying to read the message, if applicable.
func (t *inboundMsgByteThrottler) Acquire(ctx context.Context, msgSize uint64, nodeID ids.NodeID) ReleaseFunc {
	startTime := time.Now()
	defer func() {
		t.metrics.awaitingRelease.Inc()
		t.metrics.acquireLatency.Observe(float64(time.Since(startTime)))
	}()
	metadata := &msgMetadata{
		bytesNeeded: msgSize,
		msgSize:     msgSize,
		nodeID:      nodeID,
	}

	t.lock.Lock()

	// If there is already a message waiting, log the error and return
	if existingID, exists := t.nodeToWaitingMsgID[nodeID]; exists {
		t.log.Error("node already waiting on message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("messageID", existingID),
		)
		t.lock.Unlock()
		return t.metrics.awaitingRelease.Dec
	}

	// Take as many bytes as we can from the at-large allocation.
	atLargeBytesUsed := min(
		// only give as many bytes as needed
		metadata.bytesNeeded,
		// don't exceed per-node limit
		t.nodeMaxAtLargeBytes-t.nodeToAtLargeBytesUsed[nodeID],
		// don't give more bytes than are in the allocation
		t.remainingAtLargeBytes,
	)
	if atLargeBytesUsed > 0 {
		t.remainingAtLargeBytes -= atLargeBytesUsed
		t.metrics.remainingAtLargeBytes.Set(float64(t.remainingAtLargeBytes))
		metadata.bytesNeeded -= atLargeBytesUsed
		t.nodeToAtLargeBytesUsed[nodeID] += atLargeBytesUsed
		if metadata.bytesNeeded == 0 { // If we acquired enough bytes, return
			t.lock.Unlock()
			return func() {
				t.release(metadata, nodeID)
			}
		}
	}

	// Take as many bytes as we can from [nodeID]'s validator allocation.
	// Calculate [nodeID]'s validator allocation size based on its weight
	vdrAllocationSize := uint64(0)
	weight := t.vdrs.GetWeight(constants.PrimaryNetworkID, nodeID)
	if weight != 0 {
		totalWeight, err := t.vdrs.TotalWeight(constants.PrimaryNetworkID)
		if err != nil {
			t.log.Error("couldn't get total weight of primary network",
				zap.Error(err),
			)
		} else {
			vdrAllocationSize = uint64(float64(t.maxVdrBytes) * float64(weight) / float64(totalWeight))
		}
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
	vdrBytesUsed := min(t.remainingVdrBytes, metadata.bytesNeeded, vdrBytesAllowed)
	if vdrBytesUsed > 0 {
		// Mark that [nodeID] used [vdrBytesUsed] from its validator allocation
		t.nodeToVdrBytesUsed[nodeID] += vdrBytesUsed
		t.remainingVdrBytes -= vdrBytesUsed
		t.metrics.remainingVdrBytes.Set(float64(t.remainingVdrBytes))
		metadata.bytesNeeded -= vdrBytesUsed
		if metadata.bytesNeeded == 0 { // If we acquired enough bytes, return
			t.lock.Unlock()
			return func() {
				t.release(metadata, nodeID)
			}
		}
	}

	// We still haven't acquired enough bytes to read the message.
	// Wait until more bytes are released.

	// [closeOnAcquireChan] is closed when [msgSize] bytes have
	// been acquired and the message can be read.
	metadata.closeOnAcquireChan = make(chan struct{})
	t.nextMsgID++
	msgID := t.nextMsgID
	t.waitingToAcquire.Put(
		msgID,
		metadata,
	)

	t.nodeToWaitingMsgID[nodeID] = msgID
	t.lock.Unlock()

	t.metrics.awaitingAcquire.Inc()
	defer t.metrics.awaitingAcquire.Dec()

	select {
	case <-metadata.closeOnAcquireChan:
	case <-ctx.Done():
		t.lock.Lock()
		t.waitingToAcquire.Delete(msgID)
		delete(t.nodeToWaitingMsgID, nodeID)
		t.lock.Unlock()
	}

	return func() {
		t.release(metadata, nodeID)
	}
}

// Must correspond to a previous call of Acquire([msgSize], [nodeID])
func (t *inboundMsgByteThrottler) release(metadata *msgMetadata, nodeID ids.NodeID) {
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
	releasedBytes := metadata.msgSize - metadata.bytesNeeded
	vdrBytesToReturn := min(releasedBytes, vdrBytesUsed)

	// [atLargeBytesToReturn] is the number of bytes from [msgSize]
	// that will be given to the at-large allocation or a message
	// from any node currently waiting to acquire bytes.
	atLargeBytesToReturn := releasedBytes - vdrBytesToReturn
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
			msg := iter.Value()
			// From the at-large allocation, take the maximum number of bytes
			// without exceeding the per-node limit on taking from at-large pool.
			atLargeBytesGiven := min(
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
				delete(t.nodeToWaitingMsgID, msg.nodeID)

				t.waitingToAcquire.Delete(iter.Key())
			}
		}
	}

	// Get the message from [nodeID], if any, waiting to acquire
	msgID, ok := t.nodeToWaitingMsgID[nodeID]
	if vdrBytesToReturn > 0 && ok {
		msg, exists := t.waitingToAcquire.Get(msgID)
		if exists {
			// Give [msg] all the bytes we can
			bytesToGive := min(msg.bytesNeeded, vdrBytesToReturn)
			msg.bytesNeeded -= bytesToGive
			vdrBytesToReturn -= bytesToGive
			if msg.bytesNeeded == 0 {
				// Unblock the corresponding thread in Acquire
				close(msg.closeOnAcquireChan)
				delete(t.nodeToWaitingMsgID, nodeID)
				t.waitingToAcquire.Delete(msgID)
			}
		} else {
			// This should never happen
			t.log.Warn("couldn't find message",
				zap.Stringer("nodeID", nodeID),
				zap.Uint64("messageID", msgID),
			)
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

type inboundMsgByteThrottlerMetrics struct {
	acquireLatency        metric.Averager
	remainingAtLargeBytes prometheus.Gauge
	remainingVdrBytes     prometheus.Gauge
	awaitingAcquire       prometheus.Gauge
	awaitingRelease       prometheus.Gauge
}

func (m *inboundMsgByteThrottlerMetrics) initialize(reg prometheus.Registerer) error {
	errs := wrappers.Errs{}
	m.acquireLatency = metric.NewAveragerWithErrs(
		"byte_throttler_inbound_acquire_latency",
		"average time (in ns) to get space on the inbound message byte buffer",
		reg,
		&errs,
	)
	m.remainingAtLargeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "byte_throttler_inbound_remaining_at_large_bytes",
		Help: "Bytes remaining in the at-large byte buffer",
	})
	m.remainingVdrBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "byte_throttler_inbound_remaining_validator_bytes",
		Help: "Bytes remaining in the validator byte buffer",
	})
	m.awaitingAcquire = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "byte_throttler_inbound_awaiting_acquire",
		Help: "Number of inbound messages waiting to acquire space on the inbound message byte buffer",
	})
	m.awaitingRelease = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "byte_throttler_inbound_awaiting_release",
		Help: "Number of messages currently being read/handled",
	})
	errs.Add(
		reg.Register(m.remainingAtLargeBytes),
		reg.Register(m.remainingVdrBytes),
		reg.Register(m.awaitingAcquire),
		reg.Register(m.awaitingRelease),
	)
	return errs.Err
}
