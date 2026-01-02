// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package pathdb

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie/testutil"
	"github.com/ava-labs/libevm/trie/triestate"
)

// randomStateSet generates a random state change set.
func randomStateSet(n int) *triestate.Set {
	var (
		accounts = make(map[common.Address][]byte)
		storages = make(map[common.Address]map[common.Hash][]byte)
	)
	for i := 0; i < n; i++ {
		addr := testutil.RandomAddress()
		storages[addr] = make(map[common.Hash][]byte)
		for j := 0; j < 3; j++ {
			v, _ := rlp.EncodeToBytes(common.TrimLeftZeroes(testutil.RandBytes(32)))
			storages[addr][testutil.RandomHash()] = v
		}
		account := generateAccount(types.EmptyRootHash)
		accounts[addr] = types.SlimAccountRLP(account)
	}
	return triestate.New(accounts, storages, nil)
}

func makeHistory() *history {
	return newHistory(testutil.RandomHash(), types.EmptyRootHash, 0, randomStateSet(3))
}

// nolint: unused
func makeHistories(n int) []*history {
	var (
		parent = types.EmptyRootHash
		result []*history
	)
	for i := 0; i < n; i++ {
		root := testutil.RandomHash()
		h := newHistory(root, parent, uint64(i), randomStateSet(3))
		parent = root
		result = append(result, h)
	}
	return result
}

func TestEncodeDecodeHistory(t *testing.T) {
	var (
		m   meta
		dec history
		obj = makeHistory()
	)
	// check if meta data can be correctly encode/decode
	blob := obj.meta.encode()
	if err := m.decode(blob); err != nil {
		t.Fatalf("Failed to decode %v", err)
	}
	if !reflect.DeepEqual(&m, obj.meta) {
		t.Fatal("meta is mismatched")
	}

	// check if account/storage data can be correctly encode/decode
	accountData, storageData, accountIndexes, storageIndexes := obj.encode()
	if err := dec.decode(accountData, storageData, accountIndexes, storageIndexes); err != nil {
		t.Fatalf("Failed to decode, err: %v", err)
	}
	if !compareSet(dec.accounts, obj.accounts) {
		t.Fatal("account data is mismatched")
	}
	if !compareStorages(dec.storages, obj.storages) {
		t.Fatal("storage data is mismatched")
	}
	if !compareList(dec.accountList, obj.accountList) {
		t.Fatal("account list is mismatched")
	}
	if !compareStorageList(dec.storageList, obj.storageList) {
		t.Fatal("storage list is mismatched")
	}
}

func compareSet[k comparable](a, b map[k][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for key, valA := range a {
		valB, ok := b[key]
		if !ok {
			return false
		}
		if !bytes.Equal(valA, valB) {
			return false
		}
	}
	return true
}

func compareList[k comparable](a, b []k) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func compareStorages(a, b map[common.Address]map[common.Hash][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for h, subA := range a {
		subB, ok := b[h]
		if !ok {
			return false
		}
		if !compareSet(subA, subB) {
			return false
		}
	}
	return true
}

func compareStorageList(a, b map[common.Address][]common.Hash) bool {
	if len(a) != len(b) {
		return false
	}
	for h, la := range a {
		lb, ok := b[h]
		if !ok {
			return false
		}
		if !compareList(la, lb) {
			return false
		}
	}
	return true
}
