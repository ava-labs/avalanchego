package message

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/prometheus/client_golang/prometheus"
)

var _ MsgCreator = (*msgCreator)(nil)

type MsgCreator interface {
	Builder
	Parse(bytes []byte) (InboundMessage, error) // TODO ABENEGIA: not full codec interface, to be dropped
	ReturnBytes(msg interface{})
	ParentNamespace() string
}

func NewMsgCreator(metrics prometheus.Registerer, compressionEnabled bool) (MsgCreator, error) {
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
	builder := NewBuilder(codec, compressionEnabled)
	res := &msgCreator{
		Builder:         builder,
		Codec:           codec,
		byteSlicePool:   &pool,
		parentNamespace: parentNamespace,
	}
	return res, nil
}

type msgCreator struct {
	Codec
	Builder

	// Contains []byte. Used as an optimization.
	// Can be accessed by multiple goroutines concurrently.
	byteSlicePool *sync.Pool

	parentNamespace string // TODO ABENEGIA: needed to init network and chainManager with the right namespace
}

func (mc *msgCreator) ReturnBytes(msg interface{}) {
	mc.byteSlicePool.Put(msg)
}

func (mc *msgCreator) ParentNamespace() string { return mc.parentNamespace }
