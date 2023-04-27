// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	OutputOwners `serialize:"true"`
}

func (out *MintOutput) Verify() error {
	switch {
	case out == nil:
		return ErrNilOutput
	default:
		return out.OutputOwners.Verify()
	}
}

func (out *MintOutput) VerifyState() error {
	return out.Verify()
}
