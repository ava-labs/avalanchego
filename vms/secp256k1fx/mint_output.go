// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import "github.com/ava-labs/avalanchego/vms/components/verify"

var _ verify.State = &MintOutput{}

// MintOutput ...
type MintOutput struct {
	OutputOwners `serialize:"true"`
}

// Verify ...
func (out *MintOutput) Verify() error {
	switch {
	case out == nil:
		return errNilOutput
	default:
		return out.OutputOwners.Verify()
	}
}

// VerifyState ...
func (out *MintOutput) VerifyState() error { return out.Verify() }
