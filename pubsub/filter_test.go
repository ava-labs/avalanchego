// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/api"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/bloom"
	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/utils/formatting"
)

func TestAddAddressesParseAddresses(t *testing.T) {
	assert := assert.New(t)

	chainAlias := "X"
	hrp := constants.GetHRP(5)

	addrID := ids.ShortID{1}
	addrStr, err := formatting.FormatAddress(chainAlias, hrp, addrID[:])
	assert.NoError(err)

	msg := &AddAddresses{JSONAddresses: api.JSONAddresses{
		Addresses: []string{
			addrStr,
		},
	}}

	err = msg.parseAddresses()
	assert.NoError(err)

	assert.Len(msg.addressIds, 1)
	assert.Equal(addrID[:], msg.addressIds[0])
}

func TestFilterParamUpdateMulti(t *testing.T) {
	fp := NewFilterParam()

	addr1 := []byte("abc")
	addr2 := []byte("def")
	addr3 := []byte("xyz")

	if err := fp.Add(addr1, addr2, addr3); err != nil {
		t.Fatal(err)
	}
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
	mapFilter := bloom.NewMap()

	fp := NewFilterParam()
	fp.SetFilter(mapFilter)

	addr := ids.GenerateTestShortID()
	if err := fp.Add(addr[:]); err != nil {
		t.Fatal(err)
	}
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
