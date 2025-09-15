// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb/database"
)

// AccountTrie implements state.Trie for managing account states.
// There are a couple caveats to the current implementation:
//  1. `Commit` is not used as expected in the state package. The `StorageTrie` doesn't return
//     values, and we thus rely on the `AccountTrie`.
//  2. The `Hash` method actually creates the proposal, since Firewood cannot calculate
//     the hash of the trie without committing it. It is immediately dropped, and this
//     can likely be optimized.
type AccountTrie struct {
	fw           *Database
	parentRoot   common.Hash
	root         common.Hash
	reader       database.Reader
	dirtyKeys    map[string][]byte // Store dirty changes
	updateKeys   [][]byte
	updateValues [][]byte
	hasChanges   bool
}

func NewAccountTrie(root common.Hash, db *Database) (*AccountTrie, error) {
	reader, err := db.Reader(root)
	if err != nil {
		return nil, err
	}
	return &AccountTrie{
		fw:         db,
		parentRoot: root,
		reader:     reader,
		dirtyKeys:  make(map[string][]byte),
		hasChanges: true, // Start with hasChanges true to allow computing the proposal hash
	}, nil
}

// GetAccount implements state.Trie.
func (a *AccountTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()

	// First check if there's a pending update for this account
	if updateValue, exists := a.dirtyKeys[string(key)]; exists {
		// If the value is empty, it indicates deletion
		// Invariant: All encoded values have length > 0
		if len(updateValue) == 0 {
			return nil, nil
		}
		// Decode and return the updated account
		account := new(types.StateAccount)
		err := rlp.DecodeBytes(updateValue, account)
		return account, err
	}

	// No pending update found, read from the underlying reader
	accountBytes, err := a.reader.Node(common.Hash{}, key, common.Hash{})
	if err != nil {
		return nil, err
	}

	if accountBytes == nil {
		return nil, nil
	}

	// Decode the account node
	account := new(types.StateAccount)
	err = rlp.DecodeBytes(accountBytes, account)
	return account, err
}

// GetStorage implements state.Trie.
func (a *AccountTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
	// If the account has been deleted, we should return nil
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	if val, exists := a.dirtyKeys[string(accountKey)]; exists && len(val) == 0 {
		return nil, nil
	}

	var combinedKey [2 * common.HashLength]byte
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	// Check if there's a pending update for this storage slot
	if updateValue, exists := a.dirtyKeys[string(combinedKey[:])]; exists {
		// If the value is empty, it indicates deletion
		if len(updateValue) == 0 {
			return nil, nil
		}
		// Decode and return the updated storage value
		_, decoded, _, err := rlp.Split(updateValue)
		return decoded, err
	}

	// No pending update found, read from the underlying reader
	storageBytes, err := a.reader.Node(common.Hash{}, combinedKey[:], common.Hash{})
	if err != nil || storageBytes == nil {
		return nil, err
	}

	// Decode the storage value
	_, decoded, _, err := rlp.Split(storageBytes)
	return decoded, err
}

// UpdateAccount implements state.Trie.
func (a *AccountTrie) UpdateAccount(addr common.Address, account *types.StateAccount) error {
	// Queue the keys and values for later commit
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	data, err := rlp.EncodeToBytes(account)
	if err != nil {
		return err
	}
	a.dirtyKeys[string(key)] = data
	a.updateKeys = append(a.updateKeys, key)
	a.updateValues = append(a.updateValues, data)
	a.hasChanges = true // Mark that there are changes to commit
	return nil
}

// UpdateStorage implements state.Trie.
func (a *AccountTrie) UpdateStorage(addr common.Address, key []byte, value []byte) error {
	var combinedKey [2 * common.HashLength]byte
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	data, err := rlp.EncodeToBytes(value)
	if err != nil {
		return err
	}

	// Queue the keys and values for later commit
	a.dirtyKeys[string(combinedKey[:])] = data
	a.updateKeys = append(a.updateKeys, combinedKey[:])
	a.updateValues = append(a.updateValues, data)
	a.hasChanges = true // Mark that there are changes to commit
	return nil
}

// DeleteAccount implements state.Trie.
func (a *AccountTrie) DeleteAccount(addr common.Address) error {
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	// Queue the key for deletion
	a.dirtyKeys[string(key)] = []byte{}
	a.updateKeys = append(a.updateKeys, key)
	a.updateValues = append(a.updateValues, []byte{}) // Empty value indicates deletion
	a.hasChanges = true                               // Mark that there are changes to commit
	return nil
}

// DeleteStorage implements state.Trie.
func (a *AccountTrie) DeleteStorage(addr common.Address, key []byte) error {
	var combinedKey [2 * common.HashLength]byte
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	// Queue the key for deletion
	a.dirtyKeys[string(combinedKey[:])] = []byte{}
	a.updateKeys = append(a.updateKeys, combinedKey[:])
	a.updateValues = append(a.updateValues, []byte{}) // Empty value indicates deletion
	a.hasChanges = true                               // Mark that there are changes to commit
	return nil
}

// Hash implements state.Trie.
func (a *AccountTrie) Hash() common.Hash {
	hash, err := a.hash()
	if err != nil {
		log.Error("Failed to hash account trie", "error", err)
		return common.Hash{}
	}
	return hash
}

func (a *AccountTrie) hash() (common.Hash, error) {
	// If we haven't already hashed, we need to do so.
	if a.hasChanges {
		root, err := a.fw.getProposalHash(a.parentRoot, a.updateKeys, a.updateValues)
		if err != nil {
			return common.Hash{}, err
		}
		a.root = root
		a.hasChanges = false // Avoid re-hashing until next update
	}
	return a.root, nil
}

// Commit implements state.Trie.
func (a *AccountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	// Get the hash of the trie.
	hash, err := a.hash()
	if err != nil {
		return common.Hash{}, nil, err
	}

	// Create the NodeSet. This will be sent to `triedb.Update` later.
	nodeset := trienode.NewNodeSet(a.parentRoot)
	for i, key := range a.updateKeys {
		nodeset.AddNode(key, &trienode.Node{
			Blob: a.updateValues[i],
		})
	}

	return hash, nodeset, nil
}

// UpdateContractCode implements state.Trie.
// Contract code is controlled by rawdb, so we don't need to do anything here.
func (*AccountTrie) UpdateContractCode(common.Address, common.Hash, []byte) error {
	return nil
}

// GetKey implements state.Trie.
func (*AccountTrie) GetKey([]byte) []byte {
	return nil // Not implemented, as this is only used in APIs
}

// NodeIterator implements state.Trie.
func (*AccountTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errors.New("NodeIterator not implemented for Firewood")
}

// Prove implements state.Trie.
func (*AccountTrie) Prove([]byte, ethdb.KeyValueWriter) error {
	return errors.New("Prove not implemented for Firewood")
}

func (a *AccountTrie) Copy() *AccountTrie {
	// Create a new AccountTrie with the same root and reader
	newTrie := &AccountTrie{
		fw:           a.fw,
		parentRoot:   a.parentRoot,
		root:         a.root,
		reader:       a.reader, // Share the same reader
		hasChanges:   a.hasChanges,
		dirtyKeys:    make(map[string][]byte, len(a.dirtyKeys)),
		updateKeys:   make([][]byte, len(a.updateKeys)),
		updateValues: make([][]byte, len(a.updateValues)),
	}

	// Deep copy dirtyKeys map
	for k, v := range a.dirtyKeys {
		newTrie.dirtyKeys[k] = append([]byte{}, v...)
	}

	// Deep copy updateKeys and updateValues slices
	for i := range a.updateKeys {
		newTrie.updateKeys[i] = append([]byte{}, a.updateKeys[i]...)
		newTrie.updateValues[i] = append([]byte{}, a.updateValues[i]...)
	}

	return newTrie
}
