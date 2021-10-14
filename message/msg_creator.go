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
	Parse(bytes []byte) (InboundMessage, error)

	ReturnBytes(msg interface{})

	ParentNamespace() string // needed to init network and chainManager with the right namespace
}

type creator struct {
	Codec
	OutboundMsgBuilder
	InboundMsgBuilder

	// Contains []byte. Used as an optimization.
	// Can be accessed by multiple goroutines concurrently.
	byteSlicePool *sync.Pool

	parentNamespace string
}

func NewCreator(metrics prometheus.Registerer, compressionEnabled bool) (Creator, error) {
	parentNamespace := fmt.Sprintf("%s_network", constants.PlatformName)
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
		Codec:              codec,
		byteSlicePool:      &pool,
		parentNamespace:    parentNamespace,
	}
	return res, nil
}

func (mc *creator) ReturnBytes(msg interface{}) {
	mc.byteSlicePool.Put(msg)
}

func (mc *creator) ParentNamespace() string { return mc.parentNamespace }
