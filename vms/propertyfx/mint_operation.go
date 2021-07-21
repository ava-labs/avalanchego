package propertyfx

import (
	"errors"

	"github.com/ava-labs/avalanchego/snow"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errNilMintOperation = errors.New("nil mint operation")

// MintOperation ...
type MintOperation struct {
	MintInput   secp256k1fx.Input `serialize:"true" json:"mintInput"`
	MintOutput  MintOutput        `serialize:"true" json:"mintOutput"`
	OwnedOutput OwnedOutput       `serialize:"true" json:"ownedOutput"`
}

func (op *MintOperation) InitCtx(ctx *snow.Context) {
	if ctx == nil {
		return
	}

	op.MintOutput.OutputOwners.InitCtx(ctx)
	op.OwnedOutput.OutputOwners.InitCtx(ctx)
}

// Outs ...
func (op *MintOperation) Outs() []verify.State {
	return []verify.State{
		&op.MintOutput,
		&op.OwnedOutput,
	}
}

// Verify ...
func (op *MintOperation) Verify() error {
	switch {
	case op == nil:
		return errNilMintOperation
	default:
		return verify.All(&op.MintInput, &op.MintOutput, &op.OwnedOutput)
	}
}
