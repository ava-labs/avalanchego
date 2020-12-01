package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNilFreezeOperation   = errors.New("freeze operation is nil")
	errNilUnfreezeOperation = errors.New("unfreeze operation is nil")
)

// FreezeOutput's presence in the UTXO set denotes that the
// corresponding asset is frozen
type FreezeOutput struct{}

// Verify ...
func (out *FreezeOutput) Verify() error { return nil }

// VerifyState ...
func (out *FreezeOutput) VerifyState() error { return nil }

// UnfreezeOutput's presence in the UTXO set denotes that the
// corresponding asset is unfrozen
type UnfreezeOutput struct{}

// Verify ...
func (out *UnfreezeOutput) Verify() error { return nil }

// VerifyState ...
func (out *UnfreezeOutput) VerifyState() error { return nil }

// FreezeOperation consumes an AssetManagerOutput
// and produces an AssetManagerOutput and FreezeOutput
type FreezeOperation struct {
	Input              Input              `serialize:"true"`
	FreezeOutput       FreezeOutput       `serialize:"true"`
	AssetManagerOutput AssetManagerOutput `serialize:"true"`
}

// Verify ...
func (op *FreezeOperation) Verify() error {
	switch {
	case op == nil:
		return errNilFreezeOperation
	default:
		return verify.All(&op.Input, &op.FreezeOutput, &op.AssetManagerOutput)
	}
}

// Outs ...
func (op *FreezeOperation) Outs() []verify.State {
	return []verify.State{&op.FreezeOutput, &op.AssetManagerOutput}
}

// UnfreezeOperation consumes an AssetManagerOutput
// and produces an AssetManagerOutput and UnfreezeOutput
type UnfreezeOperation struct {
	Input              Input              `serialize:"true"`
	UnfreezeOutput     UnfreezeOutput     `serialize:"true"`
	AssetManagerOutput AssetManagerOutput `serialize:"true"`
}

// Verify ...
func (op *UnfreezeOperation) Verify() error {
	switch {
	case op == nil:
		return errNilUnfreezeOperation
	default:
		return verify.All(&op.Input, &op.UnfreezeOutput, &op.AssetManagerOutput)
	}
}

// Outs ...
func (op *UnfreezeOperation) Outs() []verify.State {
	return []verify.State{&op.UnfreezeOutput, &op.AssetManagerOutput}
}
