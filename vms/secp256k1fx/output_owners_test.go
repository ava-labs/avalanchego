// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMintOutputOwnersVerifyNil(t *testing.T) {
	assert := assert.New(t)
	out := (*OutputOwners)(nil)
	assert.ErrorIs(out.Verify(), errNilOutput)
}

func TestMintOutputOwnersExactEquals(t *testing.T) {
	assert := assert.New(t)
	out0 := (*OutputOwners)(nil)
	out1 := (*OutputOwners)(nil)
	assert.True(out0.Equals(out1))
}

func TestMintOutputOwnersNotEqual(t *testing.T) {
	assert := assert.New(t)
	out0 := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.ShortEmpty,
		},
	}
	out1 := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			{1},
		},
	}
	assert.False(out0.Equals(out1))
}

func TestMintOutputOwnersNotSorted(t *testing.T) {
	assert := assert.New(t)
	out := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			{1},
			{0},
		},
	}
	assert.ErrorIs(out.Verify(), errAddrsNotSortedUnique)
	out.Sort()
	assert.NoError(out.Verify())
}

func TestMarshalJSONRequiresCtxWhenAddrsArePresent(t *testing.T) {
	assert := assert.New(t)
	out := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			{1},
			{0},
		},
	}

	_, err := out.MarshalJSON()
	assert.ErrorIs(err, errMarshal)
}

func TestMarshalJSONDoesNotRequireCtxWhenAddrsAreAbsent(t *testing.T) {
	assert := assert.New(t)
	out := &OutputOwners{
		Threshold: 1,
		Locktime:  2,
		Addrs:     []ids.ShortID{},
	}

	b, err := out.MarshalJSON()
	assert.NoError(err)

	jsonData := string(b)
	assert.Equal(jsonData, "{\"addresses\":[],\"locktime\":2,\"threshold\":1}")
}
