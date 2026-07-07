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
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
)

func accountKey(addr common.Address) []byte {
	return crypto.Keccak256Hash(addr.Bytes()).Bytes() // inlined so allocation is avoided
}

func storageKey(addr common.Address, key []byte) []byte {
	var combinedKey [2 * common.HashLength]byte

	hasher := crypto.NewKeccakState()
	_, _ = hasher.Write(addr[:])
	_, _ = hasher.Read(combinedKey[:common.HashLength])

	hasher.Reset()
	_, _ = hasher.Write(key)
	_, _ = hasher.Read(combinedKey[common.HashLength:])
	return combinedKey[:]
}

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
	accountBytes, err := a.reader.Get(accountKey(addr))
	if err != nil || accountBytes == nil {
		return nil, err
	}

	// Decode the account node
	account := new(types.StateAccount)
	err = rlp.DecodeBytes(accountBytes, account)
	return account, err
}

// UpdateAccount replaces or creates the state account associated with an address.
func (a *baseTrie) UpdateAccount(addr common.Address, account *types.StateAccount) error {
	data, err := rlp.EncodeToBytes(account)
	if err != nil {
		return err
	}
	a.updateOps = append(a.updateOps, ffi.Put(accountKey(addr), data))
	a.hasChanges = true
	return nil
}

// DeleteAccount removes the state account associated with an address AND its storage trie.
func (a *baseTrie) DeleteAccount(addr common.Address) error {
	a.updateOps = append(a.updateOps, ffi.PrefixDelete(accountKey(addr)))
	a.hasChanges = true
	return nil
}

// GetStorage returns the value associated with a storage key for a given account address.
// Returns (nil, nil) if the slot does not exist.
func (a *baseTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
	storageBytes, err := a.reader.Get(storageKey(addr, key))
	if err != nil || storageBytes == nil {
		return nil, err
	}

	// avoids the significant overhead of [rlp.DecodeBytes] - 97% faster
	_, decoded, _, err := rlp.Split(storageBytes)
	return decoded, err
}

// UpdateStorage replaces or creates the value associated with a storage key for a given account address.
func (a *baseTrie) UpdateStorage(addr common.Address, key []byte, value []byte) error {
	data, err := rlp.EncodeToBytes(value)
	if err != nil {
		return err
	}

	a.updateOps = append(a.updateOps, ffi.Put(storageKey(addr, key), data))
	a.hasChanges = true
	return nil
}

// DeleteStorage removes the value associated with a storage key for a given account address.
func (a *baseTrie) DeleteStorage(addr common.Address, key []byte) error {
	a.updateOps = append(a.updateOps, ffi.Delete(storageKey(addr, key)))
	a.hasChanges = true
	return nil
}

// UpdateContractCode returns nil.
//
// For hash-based tries, the contract code is managed externally to the trie in
// the rawdb, so Firewood doesn't need to do anything here. This function only
// exists to support the VerkleTrie implementation, which has the whole contract
// stored in the trie.
func (*baseTrie) UpdateContractCode(common.Address, common.Hash, []byte) error {
	return nil
}

// GetKey returns nil.
//
// If preimages were supported, GetKey would need to return the sha3 preimage of
// the provided hashed key that was previously used to store a value.
func (*baseTrie) GetKey([]byte) []byte {
	return nil
}

// copy creates a copy of the baseTrie fields with the given reader.
func (a *baseTrie) copy(reader trieReader) *baseTrie {
	return &baseTrie{
		reader:     reader,
		root:       a.root,
		hasChanges: len(a.updateOps) > 0,
		updateOps:  slices.Clone(a.updateOps), // each ffi.BatchOp is read-only, safe to shallow copy
	}
}

var (
	errNodeIteratorNotImplemented = errors.New("NodeIterator not implemented for Firewood")
	errProveNotImplemented        = errors.New("Prove not implemented for Firewood")
)

// NodeIterator returns an error.
//
// Node iteration is only used for debug APIs, snapshot generation, and offline
// pruning. SAE does not support the debug APIs and Firewood does not require
// snapshots or offline pruning.
func (*baseTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errNodeIteratorNotImplemented
}
