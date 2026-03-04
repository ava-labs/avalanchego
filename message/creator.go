// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/compression"
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

func NewCreator(
	metrics prometheus.Registerer,
	compressionType compression.Type,
	maxMessageTimeout time.Duration,
) (Creator, error) {
	builder, err := newMsgBuilder(
		metrics,
		maxMessageTimeout,
	)
	if err != nil {
		return nil, err
	}

	return &creator{
		OutboundMsgBuilder: newOutboundBuilder(compressionType, builder),
		InboundMsgBuilder:  newInboundBuilder(builder),
	}, nil
}
