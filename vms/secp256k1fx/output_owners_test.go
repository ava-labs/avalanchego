// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMintOutputOwnersVerifyNil(t *testing.T) {
	require := require.New(t)
	out := (*OutputOwners)(nil)
	require.ErrorIs(out.Verify(), errNilOutput)
}

func TestMintOutputOwnersExactEquals(t *testing.T) {
	require := require.New(t)
	out0 := (*OutputOwners)(nil)
	out1 := (*OutputOwners)(nil)
	require.True(out0.Equals(out1))
}

func TestMintOutputOwnersNotEqual(t *testing.T) {
	require := require.New(t)
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
	require.False(out0.Equals(out1))
}

func TestMintOutputOwnersNotSorted(t *testing.T) {
	require := require.New(t)
	out := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			{1},
			{0},
		},
	}
	require.ErrorIs(out.Verify(), errAddrsNotSortedUnique)
	out.Sort()
	require.NoError(out.Verify())
}

func TestMarshalJSONRequiresCtxWhenAddrsArePresent(t *testing.T) {
	require := require.New(t)
	out := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			{1},
			{0},
		},
	}

	_, err := out.MarshalJSON()
	require.ErrorIs(err, errMarshal)
}

func TestMarshalJSONDoesNotRequireCtxWhenAddrsAreAbsent(t *testing.T) {
	require := require.New(t)
	out := &OutputOwners{
		Threshold: 1,
		Locktime:  2,
		Addrs:     []ids.ShortID{},
	}

	b, err := out.MarshalJSON()
	require.NoError(err)

	jsonData := string(b)
	require.Equal(jsonData, "{\"addresses\":[],\"locktime\":2,\"threshold\":1}")
}
