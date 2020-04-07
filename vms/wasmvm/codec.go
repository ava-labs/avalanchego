package wasmvm

import (
	codecLib "github.com/ava-labs/gecko/vms/components/codec"
)

var codec codecLib.Codec

func init() {
	codec = codecLib.NewDefault()
}
