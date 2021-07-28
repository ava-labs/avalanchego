// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/snow"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNoValueOutput              = errors.New("output has no value")
	_                verify.State = &OutputOwners{}
)

// TransferOutput ...
type TransferOutput struct {
	Amt uint64 `serialize:"true" json:"amount"`

	OutputOwners `serialize:"true"`
}

func (out *TransferOutput) InitCtx(ctx *snow.Context) {
	out.OutputOwners.InitCtx(ctx)
}

// Amount returns the quantity of the asset this output consumes
func (out *TransferOutput) Amount() uint64 { return out.Amt }

// Verify ...
func (out *TransferOutput) Verify() error {
	switch {
	case out == nil:
		return errNilOutput
	case out.Amt == 0:
		return errNoValueOutput
	default:
		return out.OutputOwners.Verify()
	}
}

// VerifyState ...
func (out *TransferOutput) VerifyState() error { return out.Verify() }

// Owners ...
func (out *TransferOutput) Owners() interface{} { return &out.OutputOwners }
