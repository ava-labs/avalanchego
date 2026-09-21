// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var _ Creator = (*creator)(nil)

type Creator interface {
	OutboundMsgBuilder
	InboundMsgBuilder
}

type creator struct {
	OutboundMsgBuilder
	InboundMsgBuilder
}

// NewCreator returns a Creator for messages up to
// [constants.DefaultMaxMessageSize].
func NewCreator(
	metrics prometheus.Registerer,
	compressionType compression.Type,
	maxMessageTimeout time.Duration,
) (Creator, error) {
	return NewCreatorWithMaxMessageSize(
		metrics,
		compressionType,
		maxMessageTimeout,
		constants.DefaultMaxMessageSize,
	)
}

// NewCreatorWithMaxMessageSize returns a Creator whose codec accepts messages
// up to [maxMessageSize]. Only the elevated message stack needs a value above
// the default; see [subnets.LargeMessagesConfig].
func NewCreatorWithMaxMessageSize(
	metrics prometheus.Registerer,
	compressionType compression.Type,
	maxMessageTimeout time.Duration,
	maxMessageSize int64,
) (Creator, error) {
	builder, err := newMsgBuilderWithMaxMessageSize(
		metrics,
		maxMessageTimeout,
		maxMessageSize,
	)
	if err != nil {
		return nil, err
	}

	return &creator{
		OutboundMsgBuilder: newOutboundBuilder(compressionType, builder),
		InboundMsgBuilder:  newInboundBuilder(builder),
	}, nil
}
