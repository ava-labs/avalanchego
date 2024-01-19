// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/pubsub/bloom"
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

	require.NoError(msg.parseAddresses())

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
	require.Len(fp.set, 3)
	require.Contains(fp.set, string(addr1))
	require.Contains(fp.set, string(addr2))
	require.Contains(fp.set, string(addr3))
}

func TestFilterParam(t *testing.T) {
	require := require.New(t)

	mapFilter := bloom.NewMap()

	fp := NewFilterParam()
	fp.SetFilter(mapFilter)

	addr := ids.GenerateTestShortID()
	require.NoError(fp.Add(addr[:]))
	require.True(fp.Check(addr[:]))
	delete(fp.set, string(addr[:]))

	mapFilter.Add(addr[:])
	require.True(fp.Check(addr[:]))
	require.False(fp.Check([]byte("bye")))
}

func TestNewBloom(t *testing.T) {
	cm := &NewBloom{}
	require.False(t, cm.IsParamsValid())
}
