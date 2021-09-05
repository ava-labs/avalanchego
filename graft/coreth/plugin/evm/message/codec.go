package message

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const codecVersion uint16 = 0

// Codec does serialization and deserialization
var c codec.Manager

func init() {
	c = codec.NewDefaultManager()
	lc := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&AtomicTxNotify{}),
		lc.RegisterType(&AtomicTx{}),
		lc.RegisterType(&EthTxsNotify{}),
		lc.RegisterType(&EthTxs{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
