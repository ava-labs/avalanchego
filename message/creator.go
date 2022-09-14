// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/constants"
)

var _ Creator = &creator{}

type Creator interface {
	OutboundMsgBuilder
	InboundMsgBuilder
	InternalMsgBuilder
}

type creator struct {
	OutboundMsgBuilder
	InboundMsgBuilder
	InternalMsgBuilder
}

func NewCreator(metrics prometheus.Registerer, compressionEnabled bool, parentNamespace string, maxInboundMessageTimeout time.Duration) (Creator, error) {
	namespace := fmt.Sprintf("%s_codec", parentNamespace)
	codec, err := NewCodecWithMemoryPool(namespace, metrics, int64(constants.DefaultMaxMessageSize), maxInboundMessageTimeout)
	if err != nil {
		return nil, err
	}
	return &creator{
		OutboundMsgBuilder: NewOutboundBuilderWithPacker(codec, compressionEnabled),
		InboundMsgBuilder:  NewInboundBuilderWithPacker(codec),
		InternalMsgBuilder: NewInternalBuilder(),
	}, nil
}

// TODO: support creator with protobufs
