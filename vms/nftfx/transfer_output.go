package nftfx

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/types"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	// MaxPayloadSize is the maximum size that can be placed into a payload
	MaxPayloadSize = units.KiB
)

var (
	errNilTransferOutput              = errors.New("nil transfer output")
	errPayloadTooLarge                = errors.New("payload too large")
	_                    verify.State = &TransferOutput{}
)

type TransferOutput struct {
	GroupID                  uint32              `serialize:"true" json:"groupID"`
	Payload                  types.JSONByteSlice `serialize:"true" json:"payload"`
	secp256k1fx.OutputOwners `serialize:"true"`
}

func (out *TransferOutput) Verify() error {
	switch {
	case out == nil:
		return errNilTransferOutput
	case len(out.Payload) > MaxPayloadSize:
		return errPayloadTooLarge
	default:
		return out.OutputOwners.Verify()
	}
}

func (out *TransferOutput) VerifyState() error { return out.Verify() }
