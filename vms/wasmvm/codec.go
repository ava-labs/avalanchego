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
}
