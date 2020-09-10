package propertyfx

import (
	"github.com/ava-labs/avalanche-go/vms/components/verify"
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

// BurnOperation ...
type BurnOperation struct {
	secp256k1fx.Input `serialize:"true"`
}

// Outs ...
func (op *BurnOperation) Outs() []verify.State { return nil }
