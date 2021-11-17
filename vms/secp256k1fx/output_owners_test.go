// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMintOutputOwnersVerifyNil(t *testing.T) {
	out := (*OutputOwners)(nil)
	if err := out.Verify(); err == nil {
		t.Fatalf("OutputOwners.Verify should have returned an error due to an nil output")
	}
}

func TestMintOutputOwnersExactEquals(t *testing.T) {
	out0 := (*OutputOwners)(nil)
	out1 := (*OutputOwners)(nil)
	if !out0.Equals(out1) {
		t.Fatalf("Outputs should have equaled")
	}
}

func TestMintOutputOwnersNotEqual(t *testing.T) {
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
	if out0.Equals(out1) {
		t.Fatalf("Outputs should not have equaled")
	}
}

func TestMintOutputOwnersNotSorted(t *testing.T) {
	out := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			{1},
			{0},
		},
	}
	if err := out.Verify(); err == nil {
		t.Fatalf("Verification should have failed due to unsorted addresses")
	}
	out.Sort()
	if err := out.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestMarshalJSONRequiresCtxWhenAddrsArePresent(t *testing.T) {
	out := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			{1},
			{0},
		},
	}

	_, err := out.MarshalJSON()
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "cannot marshal without ctx")
}

func TestMarshalJSONDoesNotRequireCtxWhenAddrsAreAbsent(t *testing.T) {
	out := &OutputOwners{
		Threshold: 1,
		Locktime:  2,
		Addrs:     []ids.ShortID{},
	}

	b, err := out.MarshalJSON()
	assert.NoError(t, err)

	jsonData := string(b)
	assert.Equal(t, jsonData, "{\"addresses\":[],\"locktime\":2,\"threshold\":1}")
}
