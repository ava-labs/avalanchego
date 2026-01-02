// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ InboundMsgThrottler = (*inboundMsgThrottler)(nil)

// InboundMsgThrottler rate-limits inbound messages from the network.
type InboundMsgThrottler interface {
	// Blocks until [nodeID] can read a message of size [msgSize].
	// AddNode([nodeID], ...) must have been called since
	// the last time RemoveNode([nodeID]) was called, if any.
	// It's safe for multiple goroutines to concurrently call Acquire.
	// Returns immediately if [ctx] is canceled.  The returned release function
	// needs to be called so that any allocated resources will be released
	// invariant: There should be a maximum of 1 blocking call to Acquire for a
	//            given nodeID. Callers must enforce this invariant.
	Acquire(ctx context.Context, msgSize uint64, nodeID ids.NodeID) ReleaseFunc

	// Add a new node to this throttler.
	// Must be called before Acquire(..., [nodeID]) is called.
	// RemoveNode([nodeID]) must have been called since the last time
	// AddNode([nodeID], ...) was called, if any.
	AddNode(nodeID ids.NodeID)

	// Remove a node from this throttler.
	// AddNode([nodeID], ...) must have been called since
	// the last time RemoveNode([nodeID]) was called, if any.
	// Must be called when we stop reading messages from [nodeID].
	// It's safe for multiple goroutines to concurrently call RemoveNode.
	RemoveNode(nodeID ids.NodeID)
}

type InboundMsgThrottlerConfig struct {
	MsgByteThrottlerConfig   `json:"byteThrottlerConfig"`
	BandwidthThrottlerConfig `json:"bandwidthThrottlerConfig"`
	CPUThrottlerConfig       SystemThrottlerConfig `json:"cpuThrottlerConfig"`
	DiskThrottlerConfig      SystemThrottlerConfig `json:"diskThrottlerConfig"`
	MaxProcessingMsgsPerNode uint64                `json:"maxProcessingMsgsPerNode"`
}

// Returns a new, sybil-safe inbound message throttler.
func NewInboundMsgThrottler(
	log logging.Logger,
	registerer prometheus.Registerer,
	vdrs validators.Manager,
	throttlerConfig InboundMsgThrottlerConfig,
	resourceTracker tracker.ResourceTracker,
	cpuTargeter tracker.Targeter,
	diskTargeter tracker.Targeter,
) (InboundMsgThrottler, error) {
	byteThrottler, err := newInboundMsgByteThrottler(
		log,
		registerer,
		vdrs,
		throttlerConfig.MsgByteThrottlerConfig,
	)
	if err != nil {
		return nil, err
	}
	bufferThrottler, err := newInboundMsgBufferThrottler(
		registerer,
		throttlerConfig.MaxProcessingMsgsPerNode,
	)
	if err != nil {
		return nil, err
	}
	bandwidthThrottler, err := newBandwidthThrottler(
		log,
		registerer,
		throttlerConfig.BandwidthThrottlerConfig,
	)
	if err != nil {
		return nil, err
	}
	cpuThrottler, err := NewSystemThrottler(
		"cpu",
		registerer,
		throttlerConfig.CPUThrottlerConfig,
		resourceTracker.CPUTracker(),
		cpuTargeter,
	)
	if err != nil {
		return nil, err
	}
	diskThrottler, err := NewSystemThrottler(
		"disk",
		registerer,
		throttlerConfig.DiskThrottlerConfig,
		resourceTracker.DiskTracker(),
		diskTargeter,
	)
	if err != nil {
		return nil, err
	}
	return &inboundMsgThrottler{
		byteThrottler:      byteThrottler,
		bufferThrottler:    bufferThrottler,
		bandwidthThrottler: bandwidthThrottler,
		cpuThrottler:       cpuThrottler,
		diskThrottler:      diskThrottler,
	}, nil
}

// A sybil-safe inbound message throttler.
// Rate-limits reading of inbound messages to prevent peers from consuming
// excess resources.
// The three resources considered are:
//
//  1. An inbound message buffer, where each message that we're currently
//     processing takes up 1 unit of space on the buffer.
//  2. An inbound message byte buffer, where a message of length n
//     that we're currently processing takes up n units of space on the buffer.
//  3. Bandwidth. The bandwidth rate-limiting is implemented using a token
//     bucket, where each token is 1 byte. See BandwidthThrottler.
//
// A call to Acquire([msgSize], [nodeID]) blocks until we've secured
// enough of both these resources to read a message of size [msgSize] from
// [nodeID].
type inboundMsgThrottler struct {
	// Rate-limits based on number of messages from a given node that we're
	// currently processing.
	bufferThrottler *inboundMsgBufferThrottler
	// Rate-limits based on recent bandwidth usage
	bandwidthThrottler bandwidthThrottler
	// Rate-limits based on size of all messages from a given
	// node that we're currently processing.
	byteThrottler *inboundMsgByteThrottler
	// Rate-limits based on CPU usage caused by a given node.
	cpuThrottler SystemThrottler
	// Rate-limits based on disk usage caused by a given node.
	diskThrottler SystemThrottler
}

// Returns when we can read a message of size [msgSize] from node [nodeID].
// Release([msgSize], [nodeID]) must be called (!) when done with the message
// or when we give up trying to read the message, if applicable.
// Even if [ctx] is canceled, The returned release function
// needs to be called so that any allocated resources will be released.
func (t *inboundMsgThrottler) Acquire(ctx context.Context, msgSize uint64, nodeID ids.NodeID) ReleaseFunc {
	// Acquire space on the inbound message buffer
	bufferRelease := t.bufferThrottler.Acquire(ctx, nodeID)
	// Acquire bandwidth
	t.bandwidthThrottler.Acquire(ctx, msgSize, nodeID)
	// Wait until our CPU usage drops to an acceptable level.
	t.cpuThrottler.Acquire(ctx, nodeID)
	// Wait until our disk usage drops to an acceptable level.
	t.diskThrottler.Acquire(ctx, nodeID)
	// Acquire space on the inbound message byte buffer
	byteRelease := t.byteThrottler.Acquire(ctx, msgSize, nodeID)
	return func() {
		bufferRelease()
		byteRelease()
	}
}

// See BandwidthThrottler.
func (t *inboundMsgThrottler) AddNode(nodeID ids.NodeID) {
	t.bandwidthThrottler.AddNode(nodeID)
}

// See BandwidthThrottler.
func (t *inboundMsgThrottler) RemoveNode(nodeID ids.NodeID) {
	t.bandwidthThrottler.RemoveNode(nodeID)
}
