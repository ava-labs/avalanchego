// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import "github.com/ava-labs/avalanchego/vms/components/verify"

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	OutputOwners `serialize:"true"`
}

func (out *MintOutput) Verify() error {
	if out == nil {
		return ErrNilOutput
	}

	return out.OutputOwners.Verify()
}
