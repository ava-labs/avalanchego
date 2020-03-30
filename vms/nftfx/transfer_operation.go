package nftfx

import (
	"errors"

	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errNilTransferOperation = errors.New("nil transfer operation")
)

// TransferOperation ...
type TransferOperation struct {
	Input  secp256k1fx.Input `serialize:"true"`
	Output TransferOutput    `serialize:"true"`
}

// Outs ...
func (op *TransferOperation) Outs() []verify.Verifiable {
	return []verify.Verifiable{&op.Output}
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
