// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestMintOperationVerifyNil(t *testing.T) {
	assert := assert.New(t)
	op := (*MintOperation)(nil)
	assert.ErrorIs(op.Verify(), errNilMintOperation)
}

func TestMintOperationOuts(t *testing.T) {
	assert := assert.New(t)
	op := &MintOperation{
		MintInput: Input{
			SigIndices: []uint32{0},
		},
		MintOutput: MintOutput{
			OutputOwners: OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					addr,
				},
			},
		},
		TransferOutput: TransferOutput{
			Amt: 1,
			OutputOwners: OutputOwners{
				Locktime:  0,
				Threshold: 1,
			},
		},
	}

	assert.Len(op.Outs(), 2)
}

func TestMintOperationState(t *testing.T) {
	assert := assert.New(t)
	intf := interface{}(&MintOperation{})
	_, ok := intf.(verify.State)
	assert.False(ok)
}
