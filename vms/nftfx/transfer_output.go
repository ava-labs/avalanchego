package nftfx

import (
	"errors"

	"github.com/ava-labs/avalanchego/snow"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	// MaxPayloadSize is the maximum size that can be placed into a payload
	MaxPayloadSize = 1 << 10
)

var (
	errNilTransferOutput              = errors.New("nil transfer output")
	errPayloadTooLarge                = errors.New("payload too large")
	_                    verify.State = &TransferOutput{}
)

// TransferOutput ...
type TransferOutput struct {
	GroupID                  uint32 `serialize:"true" json:"groupID"`
	Payload                  []byte `serialize:"true" json:"payload"`
	secp256k1fx.OutputOwners `serialize:"true"`
}

// InitCtx assigns the OutputOwners.ctx object to given [ctx] object
// Must be called at least once for MarshalJSON to work successfully
func (out *TransferOutput) InitCtx(ctx *snow.Context) {
	out.OutputOwners.InitCtx(ctx)
}

// Verify ...
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

// VerifyState ...
func (out *TransferOutput) VerifyState() error { return out.Verify() }
