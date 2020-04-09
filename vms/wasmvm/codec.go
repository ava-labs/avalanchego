package wasmvm

import (
	codecLib "github.com/ava-labs/gecko/vms/components/codec"
)

var codec codecLib.Codec

func init() {
	codec = codecLib.NewDefault()
	if err := codec.RegisterType(&createContractTx{}); err != nil {
		panic(err)
	}
	if err := codec.RegisterType(&invokeTx{}); err != nil {
		panic(err)
	}
	// TODO: Find a better way to do this...
	var anInt32 = int32(0)
	if err := codec.RegisterType(anInt32); err != nil {
		panic(err)
	}
	var anInt64 = int64(0)
	if err := codec.RegisterType(anInt64); err != nil {
		panic(err)
	}
}
