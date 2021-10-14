// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/prometheus/client_golang/prometheus"
)

var _ Creator = (*creator)(nil)

type Creator interface {
	OutboundMsgBuilder
	InboundMsgBuilder

	ReturnBytes(msg interface{})
}

type creator struct {
	OutboundMsgBuilder
	InboundMsgBuilder

	// Contains []byte. Used as an optimization.
	// Can be accessed by multiple goroutines concurrently.
	byteSlicePool *sync.Pool
}

func NewCreator(metrics prometheus.Registerer, compressionEnabled bool, parentNamespace string) (Creator, error) {
	namespace := fmt.Sprintf("%s_codec", parentNamespace)
	pool := sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, constants.DefaultByteSliceCap)
		},
	}
	codec, err := NewCodecWithAllocator(namespace,
		metrics,
		func() []byte {
			return pool.Get().([]byte)
		},
		int64(constants.DefaultMaxMessageSize))
	if err != nil {
		return nil, err
	}
	outBuilder := NewOutboundBuilder(codec, compressionEnabled)
	inBuilder := NewInboundBuilder(codec)
	res := &creator{
		OutboundMsgBuilder: outBuilder,
		InboundMsgBuilder:  inBuilder,
		byteSlicePool:      &pool,
	}
	return res, nil
}

func (mc *creator) ReturnBytes(msg interface{}) {
	mc.byteSlicePool.Put(msg)
}
