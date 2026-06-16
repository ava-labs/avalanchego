// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"
	"maps"
	"slices"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/utils/set"
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

	// cleared holds addresses whose current incarnation already had its stale
	// storage prefix-deleted by [baseTrie.clearStaleStorage]. An entry is
	// dropped once the incarnation's storage is flushed (an account Put with a
	// non-empty root), letting the next incarnation fire again.
	cleared set.Set[common.Address]
	// storageOpsAfterClear holds addresses with storage writes in updateOps
	// since the last PrefixDelete. The reader alone can't tell whether an
	// account has storage: a copied trie reads the pre-block revision while its
	// cloned updateOps may already carry storage writes.
	storageOpsAfterClear set.Set[common.Address]
}

// accountKey returns the Firewood key of an account: keccak(addr).
func accountKey(addr common.Address) []byte {
	return crypto.Keccak256(addr.Bytes())
}

// storageKey returns the Firewood key of a storage slot:
// keccak(addr) || keccak(key). An account's key prefixes its slots' keys, so
// [baseTrie.DeleteAccount] removes all of its storage with one PrefixDelete.
func storageKey(addr common.Address, key []byte) []byte {
	return append(crypto.Keccak256(addr.Bytes()), crypto.Keccak256(key)...)
}

// clearStaleStorage removes a prior incarnation's storage when an account is
// recreated after a same-block self-destruct. [state.StateDB] never calls
// [state.Trie.DeleteAccount] for a destructed-then-recreated account: under
// [rawdb.HashScheme] it orphans the old storage via storage-root indirection,
// which Firewood's flat keyspace lacks. So infer it — if the StateDB believes
// the account has no storage, any storage in the reader or pending updateOps
// belongs to a prior incarnation and is prefix-deleted.
//
// MUST run before the address's new slots are appended to updateOps, and fires
// at most once per incarnation: [accountTrie.Hash] replays all updateOps each
// call, so a PrefixDelete after a live incarnation's slot writes would wipe them.
func (a *baseTrie) clearStaleStorage(addr common.Address) error {
	if a.cleared.Contains(addr) {
		return nil
	}

	if !a.storageOpsAfterClear.Contains(addr) {
		prev, err := a.GetAccount(addr)
		if err != nil || prev == nil || prev.Root == types.EmptyRootHash {
			return err // no prior incarnation, or no storage to clear
		}
	}

	// DeleteAccount emits the PrefixDelete and updates the incarnation
	// bookkeeping; the caller re-puts the account after this.
	return a.DeleteAccount(addr)
}

// markStorageOp records that updateOps now contain a storage operation for the
// address. See [baseTrie.storageOpsAfterClear].
func (a *baseTrie) markStorageOp(addr common.Address) {
	a.storageOpsAfterClear.Add(addr)
}

// GetAccount returns the state account associated with an address.
// Returns (nil, nil) if the account does not exist.
func (a *baseTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
	accountBytes, err := a.reader.Get(accountKey(addr))
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
//
// A same-block recreation with no new storage reaches the trie only here, so
// [baseTrie.clearStaleStorage] runs before the account is written.
func (a *baseTrie) UpdateAccount(addr common.Address, account *types.StateAccount) error {
	if account.Root == types.EmptyRootHash {
		if err := a.clearStaleStorage(addr); err != nil {
			return err
		}
	} else {
		// A non-empty root means this incarnation's storage was hashed, so a
		// later [types.EmptyRootHash] signals a new incarnation that may fire again.
		a.cleared.Remove(addr)
	}

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
	a.cleared.Add(addr)
	a.storageOpsAfterClear.Remove(addr)
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

	// Decode the storage value
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
	a.markStorageOp(addr)
	a.hasChanges = true
	return nil
}

// DeleteStorage removes the value associated with a storage key for a given account address.
func (a *baseTrie) DeleteStorage(addr common.Address, key []byte) error {
	a.updateOps = append(a.updateOps, ffi.Delete(storageKey(addr, key)))
	a.markStorageOp(addr)
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
		reader:               reader,
		root:                 a.root,
		hasChanges:           true,
		updateOps:            slices.Clone(a.updateOps), // each ffi.BatchOp is read-only, safe to shallow copy
		cleared:              maps.Clone(a.cleared),
		storageOpsAfterClear: maps.Clone(a.storageOpsAfterClear),
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
