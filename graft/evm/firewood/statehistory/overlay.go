// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statehistory

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
)

var (
	errReadOnly      = errors.New("statehistory overlay: read-only")
	errNoHistoryView = errors.New("statehistory overlay: structural trie access (Prove/NodeIterator) unsupported over history")
)

// Overlay is a read-only state.Database that serves account/storage state as
// of the end of a fixed target block, reconstructed entirely from the flat
// history store. Keys are hashed forward (keccak) at query time. It performs
// no Merkle hashing and keeps no trie structure, so it cannot answer
// eth_getProof or the trie-iterating debug RPCs: those fail loud rather than
// silently answering about the tip.
//
// One Overlay is constructed per historical request (fixed target). It never
// replaces the live state database used for Verify/Accept.
type Overlay struct {
	store      *Store
	frontier   state.Database // contract code (rawdb) and DiskDB()/TrieDB() handles
	target     uint64
	targetRoot common.Hash
}

// NewOverlay returns a read-only view at the end of block target. frontier
// serves contract code (which lives in rawdb and is never pruned) and the
// DiskDB()/TrieDB() accessors; no state values are read from it. targetRoot
// is the header root of the target block (returned by Hash() to satisfy the
// interface; never navigated).
func NewOverlay(store *Store, frontier state.Database, target uint64, targetRoot common.Hash) *Overlay {
	return &Overlay{store: store, frontier: frontier, target: target, targetRoot: targetRoot}
}

func (o *Overlay) OpenTrie(common.Hash) (state.Trie, error) {
	return &overlayTrie{o: o}, nil
}

func (o *Overlay) OpenStorageTrie(common.Hash, common.Address, common.Hash, state.Trie) (state.Trie, error) {
	return &overlayTrie{o: o}, nil
}

func (o *Overlay) CopyTrie(state.Trie) state.Trie { return &overlayTrie{o: o} }

func (o *Overlay) ContractCode(addr common.Address, codeHash common.Hash) ([]byte, error) {
	return o.frontier.ContractCode(addr, codeHash)
}

func (o *Overlay) ContractCodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	return o.frontier.ContractCodeSize(addr, codeHash)
}

func (o *Overlay) DiskDB() ethdb.KeyValueStore { return o.frontier.DiskDB() }
func (o *Overlay) TrieDB() *triedb.Database    { return o.frontier.TrieDB() }

// overlayTrie serves historical values from the store at the overlay's target
// block. Account and storage tries are indistinguishable here (both resolve
// against the flat store), so the same type backs both.
type overlayTrie struct {
	o *Overlay
}

func (t *overlayTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
	return t.o.store.AccountAt(crypto.Keccak256Hash(addr.Bytes()), t.o.target)
}

func (t *overlayTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
	enc, err := t.o.store.StorageAt(crypto.Keccak256Hash(addr.Bytes()), crypto.Keccak256Hash(key), t.o.target)
	if err != nil || enc == nil {
		return nil, err
	}
	_, decoded, _, err := rlp.Split(enc)
	return decoded, err
}

func (t *overlayTrie) Hash() common.Hash { return t.o.targetRoot }

// GetKey has no preimage store here; callers on the read path do not rely on it.
func (t *overlayTrie) GetKey([]byte) []byte { return nil }

// --- read-only / unsupported ----------------------------------------------

func (t *overlayTrie) UpdateAccount(common.Address, *types.StateAccount) error { return errReadOnly }
func (t *overlayTrie) UpdateStorage(common.Address, []byte, []byte) error      { return errReadOnly }
func (t *overlayTrie) DeleteAccount(common.Address) error                      { return errReadOnly }
func (t *overlayTrie) DeleteStorage(common.Address, []byte) error              { return errReadOnly }
func (t *overlayTrie) UpdateContractCode(common.Address, common.Hash, []byte) error {
	return errReadOnly
}
func (t *overlayTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, errReadOnly
}
func (t *overlayTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errNoHistoryView
}
func (t *overlayTrie) Prove([]byte, ethdb.KeyValueWriter) error { return errNoHistoryView }

var (
	_ state.Database = (*Overlay)(nil)
	_ state.Trie     = (*overlayTrie)(nil)
)
