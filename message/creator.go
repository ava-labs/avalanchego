// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"time"

	"github.com/chain4travel/caminogo/utils/constants"
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

func NewCreator(metrics prometheus.Registerer, compressionEnabled bool, parentNamespace string, maxInboundMessageTimeout time.Duration) (Creator, error) {
	namespace := fmt.Sprintf("%s_codec", parentNamespace)
	codec, err := NewCodecWithMemoryPool(namespace, metrics, int64(constants.DefaultMaxMessageSize), maxInboundMessageTimeout)
	if err != nil {
		return nil, err
	}
	return &creator{
		OutboundMsgBuilder: NewOutboundBuilder(codec, compressionEnabled),
		InboundMsgBuilder:  NewInboundBuilder(codec),
		InternalMsgBuilder: NewInternalBuilder(),
	}, nil
}
