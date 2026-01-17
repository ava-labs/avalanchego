// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb/database"
)

var _ state.Trie = (*accountTrie)(nil)

// accountTrie implements [state.Trie] for managing account states.
// Although it fulfills the [state.Trie] interface, it has some important differences:
//  1. [accountTrie.Commit] is not used as expected in the state package. The `StorageTrie` doesn't return
//     values, and we thus rely on the `accountTrie`. Additionally, no [trienode.NodeSet] is
//     actually constructed, since Firewood manages nodes internally and the list of changes
//     is not needed externally.
//  2. The [accountTrie.Hash] method actually creates the [ffi.Proposal], since Firewood cannot calculate
//     the hash of the trie without committing it.
//
// Note this is not concurrent safe.
type accountTrie struct {
	fw           *TrieDB
	parentRoot   common.Hash
	root         common.Hash
	reader       database.Reader
	dirtyKeys    map[string][]byte // Store dirty changes
	updateKeys   [][]byte
	updateValues [][]byte
	hasChanges   bool
}

func newAccountTrie(root common.Hash, db *TrieDB) (*accountTrie, error) {
	reader, err := db.Reader(root)
	if err != nil {
		return nil, err
	}
	return &accountTrie{
		fw:         db,
		parentRoot: root,
		reader:     reader,
		dirtyKeys:  make(map[string][]byte),
		hasChanges: true, // Start with hasChanges true to allow computing the proposal hash
	}, nil
}

// GetAccount returns the state account associated with an address.
// - If the account has been updated, the new value is returned.
// - If the account has been deleted, (nil, nil) is returned.
// - If the account does not exist, (nil, nil) is returned.
func (a *accountTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
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

// GetStorage returns the value associated with a storage key for a given account address.
// - If the storage slot has been updated, the new value is returned.
// - If the storage slot has been deleted, (nil, nil) is returned.
// - If the storage slot does not exist, (nil, nil) is returned.
func (a *accountTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
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

// UpdateAccount replaces or creates the state account associated with an address.
// This new value will be returned for subsequent `GetAccount` calls.
func (a *accountTrie) UpdateAccount(addr common.Address, account *types.StateAccount) error {
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

// UpdateStorage replaces or creates the value associated with a storage key for a given account address.
// This new value will be returned for subsequent `GetStorage` calls.
func (a *accountTrie) UpdateStorage(addr common.Address, key []byte, value []byte) error {
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

// DeleteAccount removes the state account associated with an address.
func (a *accountTrie) DeleteAccount(addr common.Address) error {
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	// Queue the key for deletion
	a.dirtyKeys[string(key)] = nil
	a.updateKeys = append(a.updateKeys, key)
	a.updateValues = append(a.updateValues, nil) // Must use nil to indicate deletion
	a.hasChanges = true                          // Mark that there are changes to commit
	return nil
}

// DeleteStorage removes the value associated with a storage key for a given account address.
func (a *accountTrie) DeleteStorage(addr common.Address, key []byte) error {
	var combinedKey [2 * common.HashLength]byte
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	// Queue the key for deletion
	a.dirtyKeys[string(combinedKey[:])] = nil
	a.updateKeys = append(a.updateKeys, combinedKey[:])
	a.updateValues = append(a.updateValues, nil) // Must use nil to indicate deletion
	a.hasChanges = true                          // Mark that there are changes to commit
	return nil
}

// Hash returns the current hash of the state trie.
// This will create the necessary proposals to guarantee that the changes can
// later be committed. All new proposals will be tracked by the [TrieDB].
// If there are no changes since the last call, the cached root is returned.
// On error, the zero hash is returned.
func (a *accountTrie) Hash() common.Hash {
	hash, err := a.hash()
	if err != nil {
		log.Error("Failed to hash account trie", "error", err)
		return common.Hash{}
	}
	return hash
}

func (a *accountTrie) hash() (common.Hash, error) {
	// If we haven't already hashed, we need to do so.
	if a.hasChanges {
		root, err := a.fw.createProposals(a.parentRoot, a.updateKeys, a.updateValues)
		if err != nil {
			return common.Hash{}, err
		}
		a.root = root
		a.hasChanges = false // Avoid re-hashing until next update
	}
	return a.root, nil
}

// Commit returns the new root hash of the trie and an empty [trienode.NodeSet].
// The boolean input is ignored, as it is a relic of the StateTrie implementation.
// If the changes are not yet already tracked by the [TrieDB], they are created.
func (a *accountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	// Get the hash of the trie.
	// Ensures all changes are tracked by the Database.
	hash, err := a.hash()
	if err != nil {
		return common.Hash{}, nil, err
	}

	set := trienode.NewNodeSet(common.Hash{})
	return hash, set, nil
}

// UpdateContractCode implements state.Trie.
// Contract code is controlled by `rawdb`, so we don't need to do anything here.
// This always returns nil.
func (*accountTrie) UpdateContractCode(common.Address, common.Hash, []byte) error {
	return nil
}

// GetKey implements state.Trie.
// Preimages are not yet supported in Firewood.
// It always returns nil.
func (*accountTrie) GetKey([]byte) []byte {
	return nil
}

// NodeIterator implements state.Trie.
// Firewood does not support iterating over internal nodes.
// This always returns an error.
func (*accountTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errors.New("NodeIterator not implemented for Firewood")
}

// Prove implements state.Trie.
// Firewood does not support providing key proofs.
// This always returns an error.
func (*accountTrie) Prove([]byte, ethdb.KeyValueWriter) error {
	return errors.New("Prove not implemented for Firewood")
}

// Copy creates a deep copy of the [accountTrie].
// The [database.Reader] is shared, since it is read-only.
func (a *accountTrie) Copy() *accountTrie {
	// Create a new AccountTrie with the same root and reader
	newTrie := &accountTrie{
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
