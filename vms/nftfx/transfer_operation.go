package nftfx

import (
	"errors"

	"github.com/ava-labs/avalanche-go/vms/components/verify"
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

var (
	errNilTransferOperation = errors.New("nil transfer operation")
)

// TransferOperation ...
type TransferOperation struct {
	Input  secp256k1fx.Input `serialize:"true" json:"input"`
	Output TransferOutput    `serialize:"true" json:"output"`
}

// Outs ...
func (op *TransferOperation) Outs() []verify.State {
	return []verify.State{&op.Output}
}

// Verify ...
func (op *TransferOperation) Verify() error {
	switch {
	case op == nil:
		return errNilTransferOperation
	default:
		return verify.All(&op.Input, &op.Output)
	}
}
