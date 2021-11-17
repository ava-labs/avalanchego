// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

var _ InboundMsgThrottler = &inboundMsgThrottler{}

// InboundMsgThrottler rate-limits inbound messages from the network.
type InboundMsgThrottler interface {
	BandwidthThrottler

	// Blocks until we can read a message of size [msgSize] from [nodeID].
	// For every call to Acquire([msgSize], [nodeID]), we must (!) call
	// Release([msgSize], [nodeID]) when done processing the message
	// (or when we give up trying to read the message.)
	Acquire(msgSize uint64, nodeID ids.ShortID)

	// Mark that we're done processing a message of size [msgSize]
	// from [nodeID].
	Release(msgSize uint64, nodeID ids.ShortID)
}

type InboundMsgThrottlerConfig struct {
	MsgByteThrottlerConfig
	BandwidthThrottlerConfig
	MaxProcessingMsgsPerNode uint64 `json:"maxProcessingMsgsPerNode"`
}

// Returns a new, sybil-safe inbound message throttler.
func NewInboundMsgThrottler(
	log logging.Logger,
	namespace string,
	registerer prometheus.Registerer,
	vdrs validators.Set,
	config InboundMsgThrottlerConfig,
) (InboundMsgThrottler, error) {
	byteThrottler, err := newInboundMsgByteThrottler(
		log,
		namespace,
		registerer,
		vdrs,
		config.MsgByteThrottlerConfig,
	)
	if err != nil {
		return nil, err
	}
	bufferThrottler, err := newInboundMsgBufferThrottler(
		namespace,
		registerer,
		config.MaxProcessingMsgsPerNode,
	)
	if err != nil {
		return nil, err
	}
	bandwidthThrottler, err := NewBandwidthThrottler(
		log,
		namespace,
		registerer,
		config.BandwidthThrottlerConfig,
	)
	if err != nil {
		return nil, err
	}
	return &inboundMsgThrottler{
		byteThrottler:      byteThrottler,
		bufferThrottler:    bufferThrottler,
		bandwidthThrottler: bandwidthThrottler,
	}, nil
}

// A sybil-safe inbound message throttler.
// Rate-limits reading of inbound messages to prevent peers from
// consuming excess resources.
// The three resources considered are:
// 1. An inbound message buffer, where each message that we're currently
//    processing takes up 1 unit of space on the buffer.
// 2. An inbound message byte buffer, where a message of length n
//    that we're currently processing takes up n units of space on the buffer.
// 3. Bandwidth. The bandwidth rate-limiting is implemented using a token bucket,
//    where each token is 1 byte. See BandwidthThrottler.
// A call to Acquire([msgSize], [nodeID]) blocks until we've secured
// enough of both these resources to read a message of size [msgSize] from [nodeID].
type inboundMsgThrottler struct {
	// Rate-limits based on number of messages from a given
	// node that we're currently processing.
	bufferThrottler *inboundMsgBufferThrottler
	// Rate-limits based on recent bandwidth usage
	bandwidthThrottler BandwidthThrottler
	// Rate-limits based on size of all messages from a given
	// node that we're currently processing.
	byteThrottler *inboundMsgByteThrottler
}

// Returns when we can read a message of size [msgSize] from node [nodeID].
// Release([msgSize], [nodeID]) must be called (!) when done with the message
// or when we give up trying to read the message, if applicable.
func (t *inboundMsgThrottler) Acquire(msgSize uint64, nodeID ids.ShortID) {
	// Acquire space on the inbound message buffer
	t.bufferThrottler.Acquire(nodeID)
	// Acquire bandwidth
	t.bandwidthThrottler.Acquire(msgSize, nodeID)
	// Acquire space on the inbound message byte buffer
	t.byteThrottler.Acquire(msgSize, nodeID)
}

// Must correspond to a previous call of Acquire([msgSize], [nodeID]).
// See InboundMsgThrottler interface.
func (t *inboundMsgThrottler) Release(msgSize uint64, nodeID ids.ShortID) {
	// Release space on the inbound message buffer
	t.bufferThrottler.Release(nodeID)
	// Release space on the inbound message byte buffer
	t.byteThrottler.Release(msgSize, nodeID)
}

// See BandwidthThrottler.
func (t *inboundMsgThrottler) AddNode(nodeID ids.ShortID) {
	t.bandwidthThrottler.AddNode(nodeID)
}

// See BandwidthThrottler.
func (t *inboundMsgThrottler) RemoveNode(nodeID ids.ShortID) {
	t.bandwidthThrottler.RemoveNode(nodeID)
}
