// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"encoding/binary"
	"math/rand"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/triedb/firewood"
	"github.com/ava-labs/subnet-evm/triedb/hashdb"
)

const (
	commit byte = iota
	createAccount
	updateAccount
	deleteAccount
	addStorage
	updateStorage
	deleteStorage
	maxStep
)

var stepMap = map[byte]string{
	commit:        "commit",
	createAccount: "createAccount",
	updateAccount: "updateAccount",
	deleteAccount: "deleteAccount",
	addStorage:    "addStorage",
	updateStorage: "updateStorage",
	deleteStorage: "deleteStorage",
}

type fuzzState struct {
	require *require.Assertions

	// current state
	currentAddrs               []common.Address
	currentStorageInputIndices map[common.Address]uint64
	inputCounter               uint64
	blockNumber                uint64

	// pending changes to be committed
	merkleTries []*merkleTrie
}
type merkleTrie struct {
	name             string
	ethDatabase      state.Database
	accountTrie      state.Trie
	openStorageTries map[common.Address]state.Trie
	lastRoot         common.Hash
}

func newFuzzState(t *testing.T) *fuzzState {
	r := require.New(t)

	hashState := NewDatabaseWithConfig(
		rawdb.NewMemoryDatabase(),
		&triedb.Config{
			DBOverride: hashdb.Defaults.BackendConstructor,
		})
	ethRoot := types.EmptyRootHash
	hashTr, err := hashState.OpenTrie(ethRoot)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(hashState.TrieDB().Close())
	})

	firewoodMemdb := rawdb.NewMemoryDatabase()
	fwCfg := firewood.Defaults       // copy the defaults
	fwCfg.ChainDataDir = t.TempDir() // Use a temporary directory for the Firewood
	firewoodState := NewDatabaseWithConfig(
		firewoodMemdb,
		&triedb.Config{
			DBOverride: fwCfg.BackendConstructor,
		},
	)
	fwTr, err := firewoodState.OpenTrie(ethRoot)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(firewoodState.TrieDB().Close())
	})

	return &fuzzState{
		merkleTries: []*merkleTrie{
			{
				name:             "hash",
				ethDatabase:      hashState,
				accountTrie:      hashTr,
				openStorageTries: make(map[common.Address]state.Trie),
				lastRoot:         ethRoot,
			},
			{
				name:             "firewood",
				ethDatabase:      firewoodState,
				accountTrie:      fwTr,
				openStorageTries: make(map[common.Address]state.Trie),
				lastRoot:         ethRoot,
			},
		},
		currentStorageInputIndices: make(map[common.Address]uint64),
		require:                    r,
	}
}

// commit writes the pending changes to both tries and clears the pending changes
func (fs *fuzzState) commit() {
	for _, tr := range fs.merkleTries {
		mergedNodeSet := trienode.NewMergedNodeSet()
		for addr, str := range tr.openStorageTries {
			accountStateRoot, set, err := str.Commit(false)
			fs.require.NoError(err, "failed to commit storage trie for account %s in %s", addr.Hex(), tr.name)
			// A no-op change returns a nil set, which will cause merge to panic.
			if set != nil {
				fs.require.NoError(mergedNodeSet.Merge(set), "failed to merge storage trie nodeset for account %s in %s", addr.Hex(), tr.name)
			}

			acc, err := tr.accountTrie.GetAccount(addr)
			fs.require.NoError(err, "failed to get account %s in %s", addr.Hex(), tr.name)
			// If the account was deleted, we can skip updating the account's
			// state root.
			fs.require.NotNil(acc, "account %s is nil in %s", addr.Hex(), tr.name)

			acc.Root = accountStateRoot
			fs.require.NoError(tr.accountTrie.UpdateAccount(addr, acc), "failed to update account %s in %s", addr.Hex(), tr.name)
		}

		updatedRoot, set, err := tr.accountTrie.Commit(true)
		fs.require.NoError(err, "failed to commit account trie in %s", tr.name)

		// A no-op change returns a nil set, which will cause merge to panic.
		if set != nil {
			fs.require.NoError(mergedNodeSet.Merge(set), "failed to merge account trie nodeset in %s", tr.name)
		}

		// HashDB/PathDB only allows updating the triedb if there have been changes.
		if _, ok := tr.ethDatabase.TrieDB().Backend().(*firewood.Database); ok {
			triedbopt := stateconf.WithTrieDBUpdatePayload(common.Hash{byte(int64(fs.blockNumber - 1))}, common.Hash{byte(int64(fs.blockNumber))})
			fs.require.NoError(tr.ethDatabase.TrieDB().Update(updatedRoot, tr.lastRoot, fs.blockNumber, mergedNodeSet, nil, triedbopt), "failed to update triedb in %s", tr.name)
			tr.lastRoot = updatedRoot
		} else if updatedRoot != tr.lastRoot {
			fs.require.NoError(tr.ethDatabase.TrieDB().Update(updatedRoot, tr.lastRoot, fs.blockNumber, mergedNodeSet, nil), "failed to update triedb in %s", tr.name)
			tr.lastRoot = updatedRoot
		}
		tr.openStorageTries = make(map[common.Address]state.Trie)
		fs.require.NoError(tr.ethDatabase.TrieDB().Commit(updatedRoot, true),
			"failed to commit %s: expected hashdb root %s", tr.name, fs.merkleTries[0].lastRoot.Hex())
		tr.accountTrie, err = tr.ethDatabase.OpenTrie(tr.lastRoot)
		fs.require.NoError(err, "failed to reopen account trie for %s", tr.name)
	}
	fs.blockNumber++

	// After computing the new root for each trie, we can confirm that the hashing matches
	expectedRoot := fs.merkleTries[0].lastRoot
	for i, tr := range fs.merkleTries[1:] {
		fs.require.Equalf(expectedRoot, tr.lastRoot,
			"root mismatch for %s: expected %x, got %x (trie index %d)",
			tr.name, expectedRoot.Hex(), tr.lastRoot.Hex(), i,
		)
	}
}

// createAccount generates a new, unique account and adds it to both tries and the tracked
// current state.
func (fs *fuzzState) createAccount() {
	fs.inputCounter++
	addr := common.BytesToAddress(crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, fs.inputCounter)).Bytes())
	acc := &types.StateAccount{
		Nonce:    1,
		Balance:  uint256.NewInt(100),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash[:],
	}
	fs.currentAddrs = append(fs.currentAddrs, addr)

	for _, tr := range fs.merkleTries {
		fs.require.NoError(tr.accountTrie.UpdateAccount(addr, acc), "failed to create account %s in %s", addr.Hex(), tr.name)
	}
}

// selectAccount returns a random account and account hash for the provided index
// assumes: addrIndex < len(tr.currentAddrs)
func (fs *fuzzState) selectAccount(addrIndex int) common.Address {
	return fs.currentAddrs[addrIndex]
}

// updateAccount selects a random account, increments its nonce, and adds the update
// to the pending changes for both tries.
func (fs *fuzzState) updateAccount(addrIndex int) {
	addr := fs.selectAccount(addrIndex)

	for _, tr := range fs.merkleTries {
		acc, err := tr.accountTrie.GetAccount(addr)
		fs.require.NoError(err, "failed to get account %s for update in %s", addr.Hex(), tr.name)
		fs.require.NotNil(acc, "account %s is nil for update in %s", addr.Hex(), tr.name)
		acc.Nonce++
		acc.CodeHash = crypto.Keccak256Hash(acc.CodeHash).Bytes()
		acc.Balance.Add(acc.Balance, uint256.NewInt(3))
		fs.require.NoError(tr.accountTrie.UpdateAccount(addr, acc), "failed to update account %s in %s", addr.Hex(), tr.name)
	}
}

// deleteAccount selects a random account and deletes it from both tries and the tracked
// current state.
func (fs *fuzzState) deleteAccount(accountIndex int) {
	deleteAddr := fs.selectAccount(accountIndex)
	fs.currentAddrs = slices.DeleteFunc(fs.currentAddrs, func(addr common.Address) bool {
		return deleteAddr == addr
	})
	for _, tr := range fs.merkleTries {
		fs.require.NoError(tr.accountTrie.DeleteAccount(deleteAddr), "failed to delete account %s in %s", deleteAddr.Hex(), tr.name)
		delete(tr.openStorageTries, deleteAddr) // remove any open storage trie for the deleted account
	}
}

// openStorageTrie opens the storage trie for the provided account address.
// Uses an already opened trie, if there's a pending update to the ethereum nested
// storage trie.
//
// must maintain a map of currently open storage tries, so we can defer committing them
// until commit as opposed to after each storage update.
// This mimics the actual handling of state commitments in the EVM where storage tries are all committed immediately
// before updating the account trie along with the updated storage trie roots:
// https://github.com/ava-labs/libevm/blob/0bfe4a0380c86d7c9bf19fe84368b9695fcb96c7/core/state/statedb.go#L1155
//
// If we attempt to commit the storage tries after each operation, then attempting to re-open the storage trie
// with an updated storage trie root from ethDatabase will fail since the storage trie root will not have been
// persisted yet - leading to a missing trie node error.
func (fs *fuzzState) openStorageTrie(addr common.Address, tr *merkleTrie) state.Trie {
	storageTrie, ok := tr.openStorageTries[addr]
	if ok {
		return storageTrie
	}

	acc, err := tr.accountTrie.GetAccount(addr)
	fs.require.NoError(err, "failed to get account %s for storage trie in %s", addr.Hex(), tr.name)
	fs.require.NotNil(acc, "account %s not found in %s", addr.Hex(), tr.name)
	storageTrie, err = tr.ethDatabase.OpenStorageTrie(tr.lastRoot, addr, acc.Root, tr.accountTrie)
	fs.require.NoError(err, "failed to open storage trie for %s in %s", addr.Hex(), tr.name)
	tr.openStorageTries[addr] = storageTrie
	return storageTrie
}

// addStorage selects an account and adds a new storage key-value pair to the account.
func (fs *fuzzState) addStorage(accountIndex int) {
	addr := fs.selectAccount(accountIndex)
	// Increment storageInputIndices for the account and take the next input to generate
	// a new storage key-value pair for the account.
	fs.currentStorageInputIndices[addr]++
	storageIndex := fs.currentStorageInputIndices[addr]
	key := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))
	keyHash := crypto.Keccak256Hash(key[:])
	val := crypto.Keccak256Hash(keyHash[:])

	for _, tr := range fs.merkleTries {
		str := fs.openStorageTrie(addr, tr)
		fs.require.NoError(str.UpdateStorage(addr, key[:], val[:]), "failed to add storage for account %s in %s", addr.Hex(), tr.name)
	}

	fs.currentStorageInputIndices[addr]++
}

// updateStorage selects an account and updates an existing storage key-value pair
// note: this may "update" a key-value pair that doesn't exist if it was previously deleted.
func (fs *fuzzState) updateStorage(accountIndex int, storageIndexInput uint64) {
	addr := fs.selectAccount(accountIndex)
	storageIndex := fs.currentStorageInputIndices[addr]
	storageIndex %= storageIndexInput

	storageKey := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))
	storageKeyHash := crypto.Keccak256Hash(storageKey[:])
	fs.inputCounter++
	updatedValInput := binary.BigEndian.AppendUint64(storageKeyHash[:], fs.inputCounter)
	updatedVal := crypto.Keccak256Hash(updatedValInput)

	for _, tr := range fs.merkleTries {
		str := fs.openStorageTrie(addr, tr)
		fs.require.NoError(str.UpdateStorage(addr, storageKey[:], updatedVal[:]), "failed to update storage for account %s in %s", addr.Hex(), tr.name)
	}
}

// deleteStorage selects an account and deletes an existing storage key-value pair
// note: this may "delete" a key-value pair that doesn't exist if it was previously deleted.
func (fs *fuzzState) deleteStorage(accountIndex int, storageIndexInput uint64) {
	addr := fs.selectAccount(accountIndex)
	storageIndex := fs.currentStorageInputIndices[addr]
	storageIndex %= storageIndexInput
	storageKey := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))

	for _, tr := range fs.merkleTries {
		str := fs.openStorageTrie(addr, tr)
		fs.require.NoError(str.DeleteStorage(addr, storageKey[:]), "failed to delete storage for account %s in %s", addr.Hex(), tr.name)
	}
}

func FuzzTree(f *testing.F) {
	f.Fuzz(func(t *testing.T, randSeed int64, byteSteps []byte) {
		fuzzState := newFuzzState(t)
		rand := rand.New(rand.NewSource(randSeed)) // this isn't a good fuzz test, but it is reproducible.

		for range 10 {
			fuzzState.createAccount()
		}
		fuzzState.commit()

		const maxSteps = 1000
		if len(byteSteps) > maxSteps {
			byteSteps = byteSteps[:maxSteps]
		}

		for _, step := range byteSteps {
			step %= maxStep
			t.Log(stepMap[step])
			switch step {
			case commit:
				fuzzState.commit()
			case createAccount:
				fuzzState.createAccount()
			case updateAccount:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.updateAccount(rand.Intn(len(fuzzState.currentAddrs)))
				}
			case deleteAccount:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.deleteAccount(rand.Intn(len(fuzzState.currentAddrs)))
				}
			case addStorage:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.addStorage(rand.Intn(len(fuzzState.currentAddrs)))
				}
			case updateStorage:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.updateStorage(rand.Intn(len(fuzzState.currentAddrs)), rand.Uint64())
				}
			case deleteStorage:
				if len(fuzzState.currentAddrs) > 0 {
					fuzzState.deleteStorage(rand.Intn(len(fuzzState.currentAddrs)), rand.Uint64())
				}
			default:
				require.Failf(t, "unknown step", "got: %d", step)
			}
		}
	})
}
