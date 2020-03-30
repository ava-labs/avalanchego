// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilMintOperation = errors.New("nil mint operation")
)

// MintOperation ...
type MintOperation struct {
	MintInput      Input          `serialize:"true"`
	MintOutput     MintOutput     `serialize:"true"`
	TransferOutput TransferOutput `serialize:"true"`
}

// Outs ...
func (op *MintOperation) Outs() []verify.Verifiable {
	return []verify.Verifiable{&op.MintOutput, &op.TransferOutput}
}

// Verify ...
func (op *MintOperation) Verify() error {
	switch {
	case op == nil:
		return errNilMintOperation
	default:
		return verify.All(&op.MintInput, &op.MintOutput, &op.TransferOutput)
	}
}
