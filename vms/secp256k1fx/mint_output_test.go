// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestMintOutputVerifyNil(t *testing.T) {
	assert := assert.New(t)
	out := (*MintOutput)(nil)
	assert.ErrorIs(out.Verify(), errNilOutput)
}

func TestMintOutputState(t *testing.T) {
	assert := assert.New(t)
	intf := interface{}(&MintOutput{})
	_, ok := intf.(verify.State)
	assert.True(ok)
}
