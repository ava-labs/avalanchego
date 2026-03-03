// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"
	"slices"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb/database"
)

// baseAccountTrie contains the shared state and methods for all Firewood
// account trie implementations. It provides the read/write operations that
// are identical between [accountTrie] and [reconstructedAccountTrie].
//
// Not concurrent-safe.
type baseAccountTrie struct {
	reader     database.Reader
	root       common.Hash
	dirtyKeys  map[string][]byte
	updateOps  []ffi.BatchOp
	hasChanges bool
}

// GetAccount returns the state account associated with an address.
// - If the account has been updated, the new value is returned.
// - If the account has been deleted, (nil, nil) is returned.
// - If the account does not exist, (nil, nil) is returned.
func (a *baseAccountTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
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
func (a *baseAccountTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
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
func (a *baseAccountTrie) UpdateAccount(addr common.Address, account *types.StateAccount) error {
	// Queue the keys and values for later commit
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	data, err := rlp.EncodeToBytes(account)
	if err != nil {
		return err
	}
	a.dirtyKeys[string(key)] = data
	a.updateOps = append(a.updateOps, ffi.Put(key, data))
	a.hasChanges = true // Mark that there are changes to commit
	return nil
}

// UpdateStorage replaces or creates the value associated with a storage key for a given account address.
// This new value will be returned for subsequent `GetStorage` calls.
func (a *baseAccountTrie) UpdateStorage(addr common.Address, key []byte, value []byte) error {
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
	a.updateOps = append(a.updateOps, ffi.Put(combinedKey[:], data))
	a.hasChanges = true // Mark that there are changes to commit
	return nil
}

// DeleteAccount removes the state account associated with an address.
func (a *baseAccountTrie) DeleteAccount(addr common.Address) error {
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	// Queue the key for deletion
	a.dirtyKeys[string(key)] = nil
	a.updateOps = append(a.updateOps, ffi.PrefixDelete(key)) // Remove all storage
	a.hasChanges = true                                      // Mark that there are changes to commit
	return nil
}

// DeleteStorage removes the value associated with a storage key for a given account address.
func (a *baseAccountTrie) DeleteStorage(addr common.Address, key []byte) error {
	var combinedKey [2 * common.HashLength]byte
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	// Queue the key for deletion
	a.dirtyKeys[string(combinedKey[:])] = nil
	a.updateOps = append(a.updateOps, ffi.Delete(combinedKey[:]))
	a.hasChanges = true // Mark that there are changes to commit
	return nil
}

// UpdateContractCode implements state.Trie.
// Contract code is controlled by `rawdb`, so we don't need to do anything here.
// This always returns nil.
func (*baseAccountTrie) UpdateContractCode(common.Address, common.Hash, []byte) error {
	return nil
}

// GetKey implements state.Trie.
// Preimages are not yet supported in Firewood.
// It always returns nil.
func (*baseAccountTrie) GetKey([]byte) []byte {
	return nil
}

// NodeIterator implements state.Trie.
// Firewood does not support iterating over internal nodes.
// This always returns an error.
func (*baseAccountTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errors.New("NodeIterator not implemented for Firewood")
}

// Prove implements state.Trie.
// Firewood does not support providing key proofs.
// This always returns an error.
func (*baseAccountTrie) Prove([]byte, ethdb.KeyValueWriter) error {
	return errors.New("Prove not implemented for Firewood")
}

// copy creates a deep copy of the baseAccountTrie fields.
// The [database.Reader] is shared, since it is read-only.
func (a *baseAccountTrie) copy() baseAccountTrie {
	newBase := baseAccountTrie{
		reader:     a.reader,
		root:       a.root,
		hasChanges: a.hasChanges,
		dirtyKeys:  make(map[string][]byte, len(a.dirtyKeys)),
		updateOps:  slices.Clone(a.updateOps), // each ffi.BatchOp is read-only, safe to shallow copy
	}
	for k, v := range a.dirtyKeys {
		newBase.dirtyKeys[k] = slices.Clone(v)
	}
	return newBase
}

