// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesynctest

import (
	"testing"

	"github.com/ava-labs/coreth/utils/utilstest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
)

// FillAccountsWithOverlappingStorage adds [numAccounts] randomly generated accounts to the secure trie at [root]
// and commits it to [trieDB]. For each 3 accounts created:
// - One does not have a storage trie,
// - One has a storage trie shared with other accounts (total number of shared storage tries [numOverlappingStorageRoots]),
// - One has a uniquely generated storage trie,
// returns the new trie root and a map of funded keys to StateAccount structs.
func FillAccountsWithOverlappingStorage(
	t *testing.T, trieDB *triedb.Database, root common.Hash, numAccounts int, numOverlappingStorageRoots int,
) (common.Hash, map[*utilstest.Key]*types.StateAccount) {
	storageRoots := make([]common.Hash, 0, numOverlappingStorageRoots)
	for i := 0; i < numOverlappingStorageRoots; i++ {
		storageRoot, _, _ := GenerateTrie(t, trieDB, 16, common.HashLength)
		storageRoots = append(storageRoots, storageRoot)
	}
	storageRootIndex := 0
	return FillAccounts(t, trieDB, root, numAccounts, func(t *testing.T, i int, account types.StateAccount) types.StateAccount {
		switch i % 3 {
		case 0: // unmodified account
		case 1: // account with overlapping storage root
			account.Root = storageRoots[storageRootIndex%numOverlappingStorageRoots]
			storageRootIndex++
		case 2: // account with unique storage root
			account.Root, _, _ = GenerateTrie(t, trieDB, 16, common.HashLength)
		}

		return account
	})
}
