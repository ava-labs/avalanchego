// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// See inbound_msg_throttler.go

func newInboundMsgBufferThrottler(
	registerer prometheus.Registerer,
	maxProcessingMsgsPerNode uint64,
) (*inboundMsgBufferThrottler, error) {
	t := &inboundMsgBufferThrottler{
		maxProcessingMsgsPerNode: maxProcessingMsgsPerNode,
		awaitingAcquire:          make(map[ids.NodeID]chan struct{}),
		nodeToNumProcessingMsgs:  make(map[ids.NodeID]uint64),
	}
	return t, t.metrics.initialize(registerer)
}

// Rate-limits inbound messages based on the number of
// messages from a given node that we're currently processing.
type inboundMsgBufferThrottler struct {
	lock    sync.Mutex
	metrics inboundMsgBufferThrottlerMetrics
	// Max number of messages currently processing from a
	// given node. We will stop reading messages from a
	// node until we're processing less than this many
	// messages from the node.
	// In this case, a message is "processing" if the corresponding
	// call to Acquire() has returned or is about to return,
	// but the corresponding call to Release() has not happened.
	// TODO: Different values for validators / non-validators?
	maxProcessingMsgsPerNode uint64
	// Node ID --> Number of messages from this node we're currently processing.
	// Must only be accessed when [lock] is held.
	nodeToNumProcessingMsgs map[ids.NodeID]uint64
	// Node ID --> Channel, when closed
	// causes a goroutine waiting in Acquire to return.
	// Must only be accessed when [lock] is held.
	awaitingAcquire map[ids.NodeID]chan struct{}
}

// Acquire returns when we've acquired space on the inbound message
// buffer so that we can read a message from [nodeID].
// The returned release function must be called (!) when done processing the message
// (or when we give up trying to read the message.)
//
// invariant: There should be a maximum of 1 blocking call to Acquire for a
// given nodeID. Callers must enforce this invariant.
func (t *inboundMsgBufferThrottler) Acquire(ctx context.Context, nodeID ids.NodeID) ReleaseFunc {
	startTime := time.Now()
	defer func() {
		t.metrics.acquireLatency.Observe(float64(time.Since(startTime)))
	}()

	t.lock.Lock()
	if t.nodeToNumProcessingMsgs[nodeID] < t.maxProcessingMsgsPerNode {
		t.nodeToNumProcessingMsgs[nodeID]++
		t.lock.Unlock()
		return func() {
			t.release(nodeID)
		}
	}

	// We're currently processing the maximum number of
	// messages from [nodeID]. Wait until we've finished
	// processing some messages from [nodeID].
	// [closeOnAcquireChan] will be closed inside Release()
	// when we've acquired space on the inbound message buffer
	// for this message.
	closeOnAcquireChan := make(chan struct{})
	t.awaitingAcquire[nodeID] = closeOnAcquireChan
	t.lock.Unlock()
	t.metrics.awaitingAcquire.Inc()
	defer t.metrics.awaitingAcquire.Dec()

	var releaseFunc ReleaseFunc
	select {
	case <-closeOnAcquireChan:
		t.lock.Lock()
		t.nodeToNumProcessingMsgs[nodeID]++
		releaseFunc = func() {
			t.release(nodeID)
		}
	case <-ctx.Done():
		t.lock.Lock()
		delete(t.awaitingAcquire, nodeID)
		releaseFunc = noopRelease
	}

	t.lock.Unlock()
	return releaseFunc
}

// release marks that we've finished processing a message from [nodeID]
// and can release the space it took on the inbound message buffer.
func (t *inboundMsgBufferThrottler) release(nodeID ids.NodeID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.nodeToNumProcessingMsgs[nodeID]--
	if t.nodeToNumProcessingMsgs[nodeID] == 0 {
		delete(t.nodeToNumProcessingMsgs, nodeID)
	}

	// If we're waiting to acquire space on the inbound message
	// buffer for messages from [nodeID], allow it to proceed
	// (i.e. for its call to Acquire to return.)
	if waiting, ok := t.awaitingAcquire[nodeID]; ok {
		close(waiting)
		delete(t.awaitingAcquire, nodeID)
	}
}

type inboundMsgBufferThrottlerMetrics struct {
	acquireLatency  metric.Averager
	awaitingAcquire prometheus.Gauge
}

func (m *inboundMsgBufferThrottlerMetrics) initialize(reg prometheus.Registerer) error {
	errs := wrappers.Errs{}
	m.acquireLatency = metric.NewAveragerWithErrs(
		"buffer_throttler_inbound_acquire_latency",
		"average time (in ns) to get space on the inbound message buffer",
		reg,
		&errs,
	)
	m.awaitingAcquire = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "buffer_throttler_inbound_awaiting_acquire",
		Help: "Number of inbound messages waiting to take space on the inbound message buffer",
	})
	errs.Add(
		reg.Register(m.awaitingAcquire),
	)
	return errs.Err
}
