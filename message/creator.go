// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/prometheus/client_golang/prometheus"
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

func NewCreator(metrics prometheus.Registerer, compressionEnabled bool, parentNamespace string) (Creator, error) {
	namespace := fmt.Sprintf("%s_codec", parentNamespace)
	codec, err := NewCodecWithMemoryPool(namespace, metrics, int64(constants.DefaultMaxMessageSize))
	if err != nil {
		return nil, err
	}
	return &creator{
		OutboundMsgBuilder: NewOutboundBuilder(codec, compressionEnabled),
		InboundMsgBuilder:  NewInboundBuilder(codec),
		InternalMsgBuilder: NewInternalBuilder(),
	}, nil
}
