// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
	parentNamespace string,
	compressionEnabled bool,
	maxMessageTimeout time.Duration,
) (Creator, error) {
	namespace := fmt.Sprintf("%s_codec", parentNamespace)
	builder, err := newMsgBuilder(
		namespace,
		metrics,
		maxMessageTimeout,
	)
	if err != nil {
		return nil, err
	}

	return &creator{
		OutboundMsgBuilder: newOutboundBuilder(compressionEnabled, builder),
		InboundMsgBuilder:  newInboundBuilder(builder),
	}, nil
}
