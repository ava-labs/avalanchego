// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flatfirewood

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
)

var (
	errReadOnly    = errors.New("flatfirewood: historical state is read-only")
	errNoTrieNodes = errors.New("flatfirewood: historical state has no trie nodes")
)

// InterceptStateDatabase implements [firewood.StateDatabaseInterceptor]. The
// returned database opens live roots as recording wrappers around the Firewood
// tries and roots that Firewood no longer holds as read-only views over the
// history.
func (t *TrieDB) InterceptStateDatabase(db state.Database) state.Database {
	return &stateAccessor{Database: db, tdb: t}
}

type stateAccessor struct {
	state.Database
	tdb *TrieDB
}

func (s *stateAccessor) OpenTrie(root common.Hash) (state.Trie, error) {
	tr, err := s.tdb.inner.OpenTrie(root)
	var missing *trie.MissingNodeError
	if errors.As(err, &missing) {
		height, ok, herr := s.tdb.store.heightOf(root)
		if herr != nil {
			return nil, herr
		}
		if !ok {
			return nil, err
		}
		return &historyTrie{store: s.tdb.store, height: height, root: root, destructs: map[common.Hash]destructAt{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return &accountRecorder{Trie: tr, tdb: s.tdb}, nil
}

func (s *stateAccessor) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, tr state.Trie) (state.Trie, error) {
	switch tr := tr.(type) {
	case *accountRecorder:
		st, err := s.tdb.inner.OpenStorageTrie(stateRoot, addr, accountRoot, tr.Trie)
		if err != nil {
			return nil, err
		}
		return &storageRecorder{Trie: st, account: tr}, nil
	case *historyTrie:
		return tr, nil
	default:
		return nil, fmt.Errorf("invalid account trie type: %T", tr)
	}
}

func (s *stateAccessor) CopyTrie(tr state.Trie) state.Trie {
	switch tr := tr.(type) {
	case *accountRecorder:
		return &accountRecorder{Trie: s.tdb.inner.CopyTrie(tr.Trie), tdb: tr.tdb, ops: slices.Clone(tr.ops)}
	case *storageRecorder:
		// Firewood returns nil so that the copied StateDB reopens its storage
		// tries on top of the copied account trie; preserve that.
		return s.tdb.inner.CopyTrie(tr.Trie)
	case *historyTrie:
		return tr // immutable
	default:
		panic(fmt.Sprintf("unknown trie type %T", tr))
	}
}

// accountRecorder wraps the Firewood account trie and records every mutation,
// keyed and encoded exactly as the Firewood batch op, for the history. Its
// storage tries append to the same op list.
type accountRecorder struct {
	state.Trie
	tdb *TrieDB
	ops []op
}

func hashedAccountKey(addr common.Address) []byte {
	return crypto.Keccak256(addr[:])
}

func hashedStorageKey(addr common.Address, slot []byte) []byte {
	return append(crypto.Keccak256(addr[:]), crypto.Keccak256(slot)...)
}

func (a *accountRecorder) UpdateAccount(addr common.Address, account *types.StateAccount) error {
	if err := a.Trie.UpdateAccount(addr, account); err != nil {
		return err
	}
	data, err := rlp.EncodeToBytes(account)
	if err != nil {
		return err
	}
	a.ops = append(a.ops, op{kind: opPut, key: hashedAccountKey(addr), value: data})
	return nil
}

func (a *accountRecorder) DeleteAccount(addr common.Address) error {
	if err := a.Trie.DeleteAccount(addr); err != nil {
		return err
	}
	a.ops = append(a.ops, op{kind: opDestruct, key: hashedAccountKey(addr)})
	return nil
}

func (a *accountRecorder) UpdateStorage(addr common.Address, slot, value []byte) error {
	if err := a.Trie.UpdateStorage(addr, slot, value); err != nil {
		return err
	}
	data, err := rlp.EncodeToBytes(value)
	if err != nil {
		return err
	}
	a.ops = append(a.ops, op{kind: opPut, key: hashedStorageKey(addr, slot), value: data})
	return nil
}

func (a *accountRecorder) DeleteStorage(addr common.Address, slot []byte) error {
	if err := a.Trie.DeleteStorage(addr, slot); err != nil {
		return err
	}
	a.ops = append(a.ops, op{kind: opDelete, key: hashedStorageKey(addr, slot)})
	return nil
}

// Commit commits the Firewood trie and hands the recorded ops to the
// [TrieDB] for the [TrieDB.Update] that follows in [state.StateDB.Commit].
func (a *accountRecorder) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	root, nodes, err := a.Trie.Commit(collectLeaf)
	if err != nil {
		return root, nodes, err
	}
	a.tdb.pending = &recorded{root: root, ops: a.ops}
	a.ops = nil
	return root, nodes, nil
}

type storageRecorder struct {
	state.Trie
	account *accountRecorder
}

func (s *storageRecorder) UpdateStorage(addr common.Address, slot, value []byte) error {
	if err := s.Trie.UpdateStorage(addr, slot, value); err != nil {
		return err
	}
	data, err := rlp.EncodeToBytes(value)
	if err != nil {
		return err
	}
	s.account.ops = append(s.account.ops, op{kind: opPut, key: hashedStorageKey(addr, slot), value: data})
	return nil
}

func (s *storageRecorder) DeleteStorage(addr common.Address, slot []byte) error {
	if err := s.Trie.DeleteStorage(addr, slot); err != nil {
		return err
	}
	s.account.ops = append(s.account.ops, op{kind: opDelete, key: hashedStorageKey(addr, slot)})
	return nil
}

// historyTrie serves the state as of the end of a fixed block from the
// history rows. Keys are hashed at query time; no trie structure exists, so
// proofs and node iteration are unsupported. The same type backs the account
// trie and every storage trie of the state.
type historyTrie struct {
	store  *store
	height uint64
	root   common.Hash
	// destructs memoizes the account's latest destruction at or before
	// height, saving one seek per slot read. A trie serves one goroutine.
	destructs map[common.Hash]destructAt
}

type destructAt struct {
	block uint64
	ok    bool
}

func (h *historyTrie) GetAccount(addr common.Address) (*types.StateAccount, error) {
	return h.store.accountAt(crypto.Keccak256Hash(addr[:]), h.height)
}

func (h *historyTrie) GetStorage(addr common.Address, slot []byte) ([]byte, error) {
	hashedAddr := crypto.Keccak256Hash(addr[:])
	d, ok := h.destructs[hashedAddr]
	if !ok {
		block, found, err := h.store.destructAt(hashedAddr, h.height)
		if err != nil {
			return nil, err
		}
		d = destructAt{block: block, ok: found}
		h.destructs[hashedAddr] = d
	}
	enc, err := h.store.storageAt(hashedAddr, crypto.Keccak256Hash(slot), h.height, d.block, d.ok)
	if err != nil || enc == nil {
		return nil, err
	}
	_, decoded, _, err := rlp.Split(enc)
	return decoded, err
}

func (h *historyTrie) Hash() common.Hash  { return h.root }
func (*historyTrie) GetKey([]byte) []byte { return nil }

func (*historyTrie) UpdateAccount(common.Address, *types.StateAccount) error { return errReadOnly }
func (*historyTrie) UpdateStorage(common.Address, []byte, []byte) error      { return errReadOnly }
func (*historyTrie) DeleteAccount(common.Address) error                      { return errReadOnly }
func (*historyTrie) DeleteStorage(common.Address, []byte) error              { return errReadOnly }
func (*historyTrie) UpdateContractCode(common.Address, common.Hash, []byte) error {
	return errReadOnly
}

func (*historyTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, errReadOnly
}

func (*historyTrie) NodeIterator([]byte) (trie.NodeIterator, error) { return nil, errNoTrieNodes }
func (*historyTrie) Prove([]byte, ethdb.KeyValueWriter) error       { return errNoTrieNodes }
