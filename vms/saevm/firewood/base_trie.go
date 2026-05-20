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
)

type trieReader interface {
	Get([]byte) ([]byte, error)
	Drop() error
}

var (
	_ trieReader = (*ffi.Revision)(nil)
	_ trieReader = (*ffi.Proposal)(nil)
)

// baseTrie contains the shared state and methods for all Firewood
// trie implementations. It provides the read/write operations that are
// identical between [accountTrie] and [storageTrie].
// All reads are performed based on the base [trieReader], so updates
// tracked here do not affect the values returned.
//
// Not concurrent-safe.
type baseTrie struct {
	reader trieReader
	root   common.Hash

	updateOps  []ffi.BatchOp
	hasChanges bool
}

// GetAccount returns the state account associated with an address.
// Returns (nil, nil) if the account does not exist.
func (a *baseTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()

	accountBytes, err := a.reader.Get(key)
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

// UpdateAccount replaces or creates the state account associated with an address.
func (a *baseTrie) UpdateAccount(addr common.Address, account *types.StateAccount) error {
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	data, err := rlp.EncodeToBytes(account)
	if err != nil {
		return err
	}
	a.updateOps = append(a.updateOps, ffi.Put(key, data))
	a.hasChanges = true
	return nil
}

// DeleteAccount removes the state account associated with an address AND its storage trie.
func (a *baseTrie) DeleteAccount(addr common.Address) error {
	key := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	a.updateOps = append(a.updateOps, ffi.PrefixDelete(key))
	a.hasChanges = true
	return nil
}

// GetStorage returns the value associated with a storage key for a given account address.
// Returns (nil, nil) if the slot does not exist.
func (a *baseTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
	var combinedKey [2 * common.HashLength]byte
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	storageBytes, err := a.reader.Get(combinedKey[:])
	if err != nil || storageBytes == nil {
		return nil, err
	}

	// Decode the storage value
	_, decoded, _, err := rlp.Split(storageBytes)
	return decoded, err
}

// UpdateStorage replaces or creates the value associated with a storage key for a given account address.
func (a *baseTrie) UpdateStorage(addr common.Address, key []byte, value []byte) error {
	var combinedKey [2 * common.HashLength]byte
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	data, err := rlp.EncodeToBytes(value)
	if err != nil {
		return err
	}

	a.updateOps = append(a.updateOps, ffi.Put(combinedKey[:], data))
	a.hasChanges = true
	return nil
}

// DeleteStorage removes the value associated with a storage key for a given account address.
func (a *baseTrie) DeleteStorage(addr common.Address, key []byte) error {
	var combinedKey [2 * common.HashLength]byte
	accountKey := crypto.Keccak256Hash(addr.Bytes()).Bytes()
	storageKey := crypto.Keccak256Hash(key).Bytes()
	copy(combinedKey[:common.HashLength], accountKey)
	copy(combinedKey[common.HashLength:], storageKey)

	a.updateOps = append(a.updateOps, ffi.Delete(combinedKey[:]))
	a.hasChanges = true
	return nil
}

// UpdateContractCode implements state.Trie.
// Contract code is controlled by rawdb, so we don't need to do anything here.
// This always returns nil.
func (*baseTrie) UpdateContractCode(common.Address, common.Hash, []byte) error {
	return nil
}

// GetKey implements state.Trie.
// Preimages are not supported in Firewood.
// It always returns nil.
func (*baseTrie) GetKey([]byte) []byte {
	return nil
}

// copy creates a copy of the baseTrie fields with the given reader.
func (a *baseTrie) copy(reader trieReader) *baseTrie {
	return &baseTrie{
		reader:     reader,
		root:       a.root,
		hasChanges: a.hasChanges,
		updateOps:  slices.Clone(a.updateOps), // each ffi.BatchOp is read-only, safe to shallow copy
	}
}

var (
	errNodeIteratorNotImplemented = errors.New("NodeIterator not implemented for Firewood")
	errProveNotImplemented        = errors.New("Prove not implemented for Firewood")
)

// NodeIterator implements state.Trie.
// Firewood does not support iterating over internal nodes.
// This always returns an error.
func (*baseTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errNodeIteratorNotImplemented
}

// Prove implements state.Trie.
// Firewood does not support providing key proofs.
// This always returns an error.
func (*baseTrie) Prove([]byte, ethdb.KeyValueWriter) error {
	return errProveNotImplemented
}
