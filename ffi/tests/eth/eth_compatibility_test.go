// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package eth

import (
	"encoding/binary"
	"math/rand"
	"path"
	"slices"
	"testing"

	firewood "github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
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

var (
	stepMap = map[byte]string{
		commit:        "commit",
		createAccount: "createAccount",
		updateAccount: "updateAccount",
		deleteAccount: "deleteAccount",
		addStorage:    "addStorage",
		updateStorage: "updateStorage",
		deleteStorage: "deleteStorage",
	}
)

type merkleTriePair struct {
	fwdDB       *firewood.Database
	accountTrie state.Trie
	ethDatabase state.Database

	lastRoot common.Hash
	require  *require.Assertions

	// current state
	currentAddrs               []common.Address
	currentStorageInputIndices map[common.Address]uint64
	inputCounter               uint64

	// pending changes to both firewood and eth database
	openStorageTries map[common.Address]state.Trie
	pendingFwdKeys   [][]byte
	pendingFwdVals   [][]byte
}

func newMerkleTriePair(t *testing.T) *merkleTriePair {
	r := require.New(t)

	file := path.Join(t.TempDir(), "test.db")
	cfg := firewood.DefaultConfig()
	db, err := firewood.New(file, cfg)
	r.NoError(err)

	tdb := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), triedb.HashDefaults)
	ethRoot := types.EmptyRootHash
	tr, err := tdb.OpenTrie(ethRoot)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(db.Close())
	})

	return &merkleTriePair{
		fwdDB:                      db,
		accountTrie:                tr,
		ethDatabase:                tdb,
		openStorageTries:           make(map[common.Address]state.Trie),
		currentStorageInputIndices: make(map[common.Address]uint64),
		require:                    r,
	}
}

// commit writes the pending changes to both tries and clears the pending changes
func (tr *merkleTriePair) commit() {
	mergedNodeSet := trienode.NewMergedNodeSet()
	for addr, str := range tr.openStorageTries {
		accountStateRoot, set, err := str.Commit(false)
		tr.require.NoError(err)
		// A no-op change returns a nil set, which will cause merge to panic.
		if set != nil {
			tr.require.NoError(mergedNodeSet.Merge(set))
		}

		acc, err := tr.accountTrie.GetAccount(addr)
		tr.require.NoError(err)
		// If the account was deleted, we can skip updating the account's
		// state root.
		if acc == nil {
			continue
		}

		acc.Root = accountStateRoot
		tr.require.NoError(tr.accountTrie.UpdateAccount(addr, acc))

		accHash := crypto.Keccak256(addr[:])
		tr.pendingFwdKeys = append(tr.pendingFwdKeys, accHash[:])
		updatedAccountRLP, err := rlp.EncodeToBytes(acc)
		tr.require.NoError(err)
		tr.pendingFwdVals = append(tr.pendingFwdVals, updatedAccountRLP)
	}

	updatedRoot, set, err := tr.accountTrie.Commit(true)
	tr.require.NoError(err)

	// A no-op change returns a nil set, which will cause merge to panic.
	if set != nil {
		tr.require.NoError(mergedNodeSet.Merge(set))
	}

	tr.require.NoError(tr.ethDatabase.TrieDB().Update(updatedRoot, tr.lastRoot, 0, mergedNodeSet, nil))
	tr.lastRoot = updatedRoot

	fwdRoot, err := tr.fwdDB.Update(tr.pendingFwdKeys, tr.pendingFwdVals)
	tr.require.NoError(err)
	tr.require.Equal(fwdRoot, updatedRoot[:])

	tr.pendingFwdKeys = nil
	tr.pendingFwdVals = nil
	tr.openStorageTries = make(map[common.Address]state.Trie)

	tr.accountTrie, err = tr.ethDatabase.OpenTrie(tr.lastRoot)
	tr.require.NoError(err)
}

// createAccount generates a new, unique account and adds it to both tries and the tracked
// current state.
func (tr *merkleTriePair) createAccount() {
	tr.inputCounter++
	addr := common.BytesToAddress(crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, tr.inputCounter)).Bytes())
	accHash := crypto.Keccak256Hash(addr[:])
	acc := &types.StateAccount{
		Nonce:    1,
		Balance:  uint256.NewInt(100),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash[:],
	}
	accountRLP, err := rlp.EncodeToBytes(acc)
	tr.require.NoError(err)

	err = tr.accountTrie.UpdateAccount(addr, acc)
	tr.require.NoError(err)
	tr.currentAddrs = append(tr.currentAddrs, addr)

	tr.pendingFwdKeys = append(tr.pendingFwdKeys, accHash[:])
	tr.pendingFwdVals = append(tr.pendingFwdVals, accountRLP)
}

// selectAccount returns a random account and account hash for the provided index
// assumes: addrIndex < len(tr.currentAddrs)
func (tr *merkleTriePair) selectAccount(addrIndex int) (common.Address, common.Hash) {
	addr := tr.currentAddrs[addrIndex]
	return addr, crypto.Keccak256Hash(addr[:])
}

// updateAccount selects a random account, increments its nonce, and adds the update
// to the pending changes for both tries.
func (tr *merkleTriePair) updateAccount(addrIndex int) {
	addr, accHash := tr.selectAccount(addrIndex)
	acc, err := tr.accountTrie.GetAccount(addr)
	tr.require.NoError(err)
	acc.Nonce++
	acc.CodeHash = crypto.Keccak256Hash(acc.CodeHash[:]).Bytes()
	acc.Balance.Add(acc.Balance, uint256.NewInt(3))
	accountRLP, err := rlp.EncodeToBytes(acc)
	tr.require.NoError(err)

	err = tr.accountTrie.UpdateAccount(addr, acc)
	tr.require.NoError(err)

	tr.pendingFwdKeys = append(tr.pendingFwdKeys, accHash[:])
	tr.pendingFwdVals = append(tr.pendingFwdVals, accountRLP)
}

// deleteAccount selects a random account and deletes it from both tries and the tracked
// current state.
func (tr *merkleTriePair) deleteAccount(accountIndex int) {
	deleteAddr, accHash := tr.selectAccount(accountIndex)

	tr.require.NoError(tr.accountTrie.DeleteAccount(deleteAddr))
	tr.currentAddrs = slices.DeleteFunc(tr.currentAddrs, func(addr common.Address) bool {
		return deleteAddr == addr
	})

	tr.pendingFwdKeys = append(tr.pendingFwdKeys, accHash[:])
	tr.pendingFwdVals = append(tr.pendingFwdVals, []byte{})
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
func (tr *merkleTriePair) openStorageTrie(addr common.Address) state.Trie {
	storageTrie, ok := tr.openStorageTries[addr]
	if ok {
		return storageTrie
	}

	acc, err := tr.accountTrie.GetAccount(addr)
	tr.require.NoError(err)
	storageTrie, err = tr.ethDatabase.OpenStorageTrie(tr.lastRoot, addr, acc.Root, tr.accountTrie)
	tr.require.NoError(err)
	tr.openStorageTries[addr] = storageTrie
	return storageTrie
}

// addStorage selects an account and adds a new storage key-value pair to the account.
func (tr *merkleTriePair) addStorage(accountIndex int) {
	addr, accHash := tr.selectAccount(accountIndex)
	// Increment storageInputIndices for the account and take the next input to generate
	// a new storage key-value pair for the account.
	tr.currentStorageInputIndices[addr]++
	storageIndex := tr.currentStorageInputIndices[addr]
	key := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))
	keyHash := crypto.Keccak256Hash(key[:])
	val := crypto.Keccak256Hash(keyHash[:])

	str := tr.openStorageTrie(addr)
	err := str.UpdateStorage(addr, key[:], val[:])
	tr.require.NoError(err)

	// Update storage key-value pair in firewood
	tr.pendingFwdKeys = append(tr.pendingFwdKeys, append(accHash[:], keyHash[:]...))
	encodedVal, err := rlp.EncodeToBytes(val[:])
	tr.require.NoError(err)
	tr.pendingFwdVals = append(tr.pendingFwdVals, encodedVal)

	tr.currentStorageInputIndices[addr]++
}

// updateStorage selects an account and updates an existing storage key-value pair
// note: this may "update" a key-value pair that doesn't exist if it was previously deleted.
func (tr *merkleTriePair) updateStorage(accountIndex int, storageIndexInput uint64) {
	addr, accHash := tr.selectAccount(accountIndex)
	storageIndex := tr.currentStorageInputIndices[addr]
	storageIndex %= storageIndexInput

	storageKey := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))
	storageKeyHash := crypto.Keccak256Hash(storageKey[:])
	tr.inputCounter++
	updatedValInput := binary.BigEndian.AppendUint64(storageKeyHash[:], tr.inputCounter)
	updatedVal := crypto.Keccak256Hash(updatedValInput[:])

	str := tr.openStorageTrie(addr)
	tr.require.NoError(str.UpdateStorage(addr, storageKey[:], updatedVal[:]))

	tr.pendingFwdKeys = append(tr.pendingFwdKeys, append(accHash[:], storageKeyHash[:]...))
	updatedValRLP, err := rlp.EncodeToBytes(updatedVal[:])
	tr.require.NoError(err)
	tr.pendingFwdVals = append(tr.pendingFwdVals, updatedValRLP[:])
}

// deleteStorage selects an account and deletes an existing storage key-value pair
// note: this may "delete" a key-value pair that doesn't exist if it was previously deleted.
func (tr *merkleTriePair) deleteStorage(accountIndex int, storageIndexInput uint64) {
	addr, accHash := tr.selectAccount(accountIndex)
	storageIndex := tr.currentStorageInputIndices[addr]
	storageIndex %= storageIndexInput
	storageKey := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, storageIndex))
	storageKeyHash := crypto.Keccak256Hash(storageKey[:])

	str := tr.openStorageTrie(addr)
	tr.require.NoError(str.DeleteStorage(addr, storageKey[:]))

	tr.pendingFwdKeys = append(tr.pendingFwdKeys, append(accHash[:], storageKeyHash[:]...))
	tr.pendingFwdVals = append(tr.pendingFwdVals, []byte{})
}

func FuzzFirewoodTree(f *testing.F) {
	for randSeed := range int64(5) {
		rand := rand.New(rand.NewSource(randSeed))
		steps := make([]byte, 32)
		_, err := rand.Read(steps)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(randSeed, steps)
	}
	f.Fuzz(func(t *testing.T, randSeed int64, byteSteps []byte) {
		tr := newMerkleTriePair(t)
		rand := rand.New(rand.NewSource(randSeed))

		for range 10 {
			tr.createAccount()
		}
		tr.commit()

		const maxSteps = 1000
		if len(byteSteps) > maxSteps {
			byteSteps = byteSteps[:maxSteps]
		}

		for _, step := range byteSteps {
			step = step % maxStep
			t.Log(stepMap[step])
			switch step {
			case commit:
				tr.commit()
			case createAccount:
				tr.createAccount()
			case updateAccount:
				if len(tr.currentAddrs) > 0 {
					tr.updateAccount(rand.Intn(len(tr.currentAddrs)))
				}
			case deleteAccount:
				if len(tr.currentAddrs) > 0 {
					tr.deleteAccount(rand.Intn(len(tr.currentAddrs)))
				}
			case addStorage:
				if len(tr.currentAddrs) > 0 {
					tr.addStorage(rand.Intn(len(tr.currentAddrs)))
				}
			case updateStorage:
				if len(tr.currentAddrs) > 0 {
					tr.updateStorage(rand.Intn(len(tr.currentAddrs)), rand.Uint64())
				}
			case deleteStorage:
				if len(tr.currentAddrs) > 0 {
					tr.deleteStorage(rand.Intn(len(tr.currentAddrs)), rand.Uint64())
				}
			default:
				t.Fatalf("unknown step: %d", step)
			}
		}
	})
}
