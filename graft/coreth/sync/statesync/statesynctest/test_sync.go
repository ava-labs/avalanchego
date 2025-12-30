// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesynctest

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
)

// FillAccountsWithOverlappingStorage adds [numAccounts] randomly generated accounts to the secure trie at [root]
// and commits it to [trieDB]. For each 3 accounts created:
// - One does not have a storage trie,
// - One has a storage trie shared with other accounts (total number of shared storage tries [numOverlappingStorageRoots]),
// - One has a uniquely generated storage trie,
// returns the new trie root and a map of funded keys to StateAccount structs.
// This is only safe for HashDB, as path-based DBs do not share storage tries.
func FillAccountsWithOverlappingStorage(
	t *testing.T, r *rand.Rand, s state.Database, root common.Hash, numAccounts int, numOverlappingStorageRoots int,
) (common.Hash, map[*utilstest.Key]*types.StateAccount) {
	storageRoots := make([]common.Hash, 0, numOverlappingStorageRoots)
	for i := 0; i < numOverlappingStorageRoots; i++ {
		storageRoot, _, _ := GenerateIndependentTrie(t, r, s.TrieDB(), 16, common.HashLength)
		storageRoots = append(storageRoots, storageRoot)
	}
	storageRootIndex := 0
	return FillAccounts(t, r, s, root, numAccounts, func(t *testing.T, i int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
		switch i % 3 {
		case 0: // unmodified account
		case 1: // account with overlapping storage root
			account.Root = storageRoots[storageRootIndex%numOverlappingStorageRoots]
			storageRootIndex++
		case 2: // account with unique storage root
			FillStorageForAccount(t, r, root, 26, addr, storageTr)
		}

		return account
	})
}
