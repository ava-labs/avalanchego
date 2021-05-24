package nftfx

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errNilMintOperation = errors.New("nil mint operation")

// MintOperation ...
type MintOperation struct {
	MintInput secp256k1fx.Input           `serialize:"true" json:"mintInput"`
	GroupID   uint32                      `serialize:"true" json:"groupID"`
	Payload   []byte                      `serialize:"true" json:"payload"`
	Outputs   []*secp256k1fx.OutputOwners `serialize:"true" json:"outputs"`
}

// Outs ...
func (op *MintOperation) Outs() []verify.State {
	outs := []verify.State{}
	for _, out := range op.Outputs {
		outs = append(outs, &TransferOutput{
			GroupID:      op.GroupID,
			Payload:      op.Payload,
			OutputOwners: *out,
		})
	}
	return outs
}

// Verify ...
func (op *MintOperation) Verify() error {
	switch {
	case op == nil:
		return errNilMintOperation
	case len(op.Payload) > MaxPayloadSize:
		return errPayloadTooLarge
	}

	for _, out := range op.Outputs {
		if err := out.Verify(); err != nil {
			return err
		}
	}
	return op.MintInput.Verify()
}
