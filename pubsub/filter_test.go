// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
)

func TestAddAddressesParseAddresses(t *testing.T) {
	require := require.New(t)

	chainAlias := "X"
	hrp := constants.GetHRP(5)

	addrID := ids.ShortID{1}
	addrStr, err := address.Format(chainAlias, hrp, addrID[:])
	require.NoError(err)

	msg := &AddAddresses{JSONAddresses: api.JSONAddresses{
		Addresses: []string{
			addrStr,
		},
	}}

	err = msg.parseAddresses()
	require.NoError(err)

	require.Len(msg.addressIds, 1)
	require.Equal(addrID[:], msg.addressIds[0])
}

func TestFilterParamUpdateMulti(t *testing.T) {
	require := require.New(t)

	fp := NewFilterParam()

	addr1 := []byte("abc")
	addr2 := []byte("def")
	addr3 := []byte("xyz")

	require.NoError(fp.Add(addr1, addr2, addr3))
	if len(fp.set) != 3 {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.set[string(addr1)]; !exists {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.set[string(addr2)]; !exists {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.set[string(addr3)]; !exists {
		t.Fatalf("update multi failed")
	}
}

func TestFilterParam(t *testing.T) {
	require := require.New(t)

	mapFilter := bloom.NewMap()

	fp := NewFilterParam()
	fp.SetFilter(mapFilter)

	addr := ids.GenerateTestShortID()
	require.NoError(fp.Add(addr[:]))
	if !fp.Check(addr[:]) {
		t.Fatalf("check address failed")
	}
	delete(fp.set, string(addr[:]))

	mapFilter.Add(addr[:])
	if !fp.Check(addr[:]) {
		t.Fatalf("check address failed")
	}
	if fp.Check([]byte("bye")) {
		t.Fatalf("check address failed")
	}
}

func TestNewBloom(t *testing.T) {
	cm := &NewBloom{}
	if cm.IsParamsValid() {
		t.Fatalf("new filter check failed")
	}
}
