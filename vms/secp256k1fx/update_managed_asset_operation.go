package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNilUpdateManagedAssetOperation = errors.New("UpdateManagedAssetOperation is nil")
)

// UpdateManagedAssetStatusOperation updates the status
// of a managed asset and/or mints more of this asset
type UpdateManagedAssetOperation struct {
	// Allows a managed output status to be spent
	Input `serialize:"true"`

	// New status of this managed asset
	// May be the same as the old status
	ManagedAssetStatusOutput `serialize:"true"`

	// If [Mint], this operation mints more of this asset.
	// The mint information is encoded in [TransferOutput.]
	// If [Mint] is false, all fields of [TransferOutput]
	// should be their zero values.
	Mint           bool `serialize:"true"`
	TransferOutput `serialize:"true"`
}

// Verify ...
func (op *UpdateManagedAssetOperation) Verify() error {
	if op == nil {
		return errNilUpdateManagedAssetOperation
	}
	if err := verify.All(&op.Input, &op.ManagedAssetStatusOutput); err != nil {
		return err
	}
	if op.Mint {
		if err := op.TransferOutput.Verify(); err != nil {
			return err
		}
		return nil
	}
	// If this operation does not mint, [TransferOutput] should be empty
	switch {
	case op.TransferOutput.Amt != 0:
		return errors.New("mint amount should be 0 since mint is false")
	case op.TransferOutput.Locktime != 0:
		return errors.New("mint locktime should be 0 since mint is false")
	case len(op.TransferOutput.Addrs) != 0:
		return errors.New("len(mint addresses) should be 0 since mint is false")
	case op.TransferOutput.Threshold != 0:
		return errors.New("mint threshold should be 0 since mint is false")
	}
	return nil
}

// Outs ...
func (op *UpdateManagedAssetOperation) Outs() []verify.State {
	if op.Mint {
		return []verify.State{&op.ManagedAssetStatusOutput, &op.TransferOutput}
	}
	return []verify.State{&op.ManagedAssetStatusOutput}
}
