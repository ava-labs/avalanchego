// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package plantstore holds two benchmark-only state backends for SAE, chosen
// by the state schemes [NullScheme] and [OursScheme]:
//
//   - null: Go maps, no trie, no hashing, no disk. Roots are fake but
//     deterministic (keccak(parent root || layer height)), except the genesis
//     root, which is the real Merkle root because the VM computes it itself.
//   - ours: the faster-firewood-node engine (vms/saevm/oursengine), which
//     computes the real Ethereum state root of every block.
//
// Both keep their state in memory for the life of the process: a restarted
// node starts from an empty store, commits genesis again and re-executes.
// SAE opens and closes the trie database more than once at startup (genesis,
// the recovery probe, the tracker), so the stores live in a process-wide
// registry keyed by directory and Close keeps them.
//
// SAE executes each block once, on the post-execution state of its parent, so
// a block's writes are applied when [state.StateDB.Commit] commits its account
// trie, and only on the newest state. Reads are served at the newest state and
// at a bounded window of older roots.
package plantstore

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/firewood"
)

const (
	// NullScheme selects the null store.
	NullScheme = "null"
	// OursScheme selects the ours store.
	OursScheme = "ours"

	// Window is the number of older roots, below the newest one, at which
	// state stays readable.
	Window = 128
)

var (
	errNotTip     = errors.New("plantstore: parent is not the newest state")
	errNotUpdated = errors.New("plantstore: root is not the newest state")
)

// registry keeps every store opened by this process, by directory.
var registry = struct {
	sync.Mutex
	m map[string]any
}{m: map[string]any{}}

func shared[T any](dir string, open func() (T, error)) (T, error) {
	registry.Lock()
	defer registry.Unlock()
	if s, ok := registry.m[dir]; ok {
		if t, ok := s.(T); ok {
			return t, nil
		}
		var zero T
		return zero, fmt.Errorf("plantstore: %s is open as %T", dir, s)
	}
	t, err := open()
	if err != nil {
		return t, err
	}
	registry.m[dir] = t
	return t, nil
}

// Release closes and forgets the store opened in dir, if any. The VM never
// calls it (the store lives as long as the process); tests do.
func Release(dir string) error {
	registry.Lock()
	s := registry.m[dir]
	delete(registry.m, dir)
	registry.Unlock()
	if c, ok := s.(interface{ release() error }); ok {
		return c.release()
	}
	return nil
}

// writes is what an account trie collected for one block.
type writes struct {
	accts   map[common.Address]*types.StateAccount // nil: deleted
	slots   map[common.Address]map[common.Hash]common.Hash
	deleted map[common.Address]bool // DeleteAccount: the parent's storage is gone
	code    map[common.Hash][]byte
}

func newWrites() *writes {
	return &writes{
		accts:   map[common.Address]*types.StateAccount{},
		slots:   map[common.Address]map[common.Hash]common.Hash{},
		deleted: map[common.Address]bool{},
		code:    map[common.Hash][]byte{},
	}
}

func (w *writes) empty() bool {
	return len(w.accts) == 0 && len(w.slots) == 0 && len(w.deleted) == 0
}

func (w *writes) clone() *writes {
	c := newWrites()
	for a, x := range w.accts {
		if x != nil {
			x = x.Copy()
		}
		c.accts[a] = x
	}
	for a, m := range w.slots {
		c.slots[a] = maps.Clone(m)
	}
	maps.Copy(c.deleted, w.deleted)
	maps.Copy(c.code, w.code)
	return c
}

// addrs returns every written address, ascending.
func (w *writes) addrs() []common.Address {
	out := slices.Collect(maps.Keys(w.accts))
	for a := range w.slots {
		if _, ok := w.accts[a]; !ok {
			out = append(out, a)
		}
	}
	for a := range w.deleted {
		if _, ok := w.accts[a]; !ok {
			if _, ok := w.slots[a]; !ok {
				out = append(out, a)
			}
		}
	}
	slices.SortFunc(out, func(x, y common.Address) int { return bytes.Compare(x[:], y[:]) })
	return out
}

func sortedKeys(m map[common.Hash]common.Hash) []common.Hash {
	ks := slices.Collect(maps.Keys(m))
	slices.SortFunc(ks, func(x, y common.Hash) int { return bytes.Compare(x[:], y[:]) })
	return ks
}

// backend is a store behind an [accountTrie].
type backend interface {
	known(root common.Hash) bool
	account(root common.Hash, a common.Address) (*types.StateAccount, error)
	slot(root common.Hash, a common.Address, k common.Hash) (common.Hash, error)
	// hash is the root of w on parent without changing the store; ok false
	// when the store cannot know it before commit.
	hash(parent common.Hash, w *writes) (_ common.Hash, ok bool, _ error)
	// commit applies w on parent, which MUST be the newest state.
	commit(parent common.Hash, w *writes) (common.Hash, error)
	logger() logging.Logger
}

// accessor serves the backend's tries to a [state.StateDB].
type accessor struct {
	state.Database
	b backend
}

func (s *accessor) OpenTrie(root common.Hash) (state.Trie, error) {
	if !s.b.known(root) {
		return nil, &trie.MissingNodeError{NodeHash: root}
	}
	return &accountTrie{b: s.b, parent: root, w: newWrites()}, nil
}

//nolint:revive // removing names loses context.
func (*accessor) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, self state.Trie) (state.Trie, error) {
	a, ok := self.(*accountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type: %T", self)
	}
	return storageTrie{a}, nil
}

// CopyTrie copies an account trie. A storage trie copies to nil, so the
// [state.StateDB] reopens it on the copied account trie (as the Firewood
// adapter does).
func (s *accessor) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *accountTrie:
		return t.copy()
	case storageTrie:
		return nil
	default:
		s.b.logger().Fatal("unknown trie type", zap.String("type", fmt.Sprintf("%T", t)))
		return nil
	}
}

// accountTrie is the state of one block: reads at its parent root under its
// own writes, the writes applied by Commit.
type accountTrie struct {
	b         backend
	parent    common.Hash
	w         *writes
	committed bool
	root      common.Hash
}

var (
	_ state.Trie = (*accountTrie)(nil)
	_ state.Trie = storageTrie{}
)

func (t *accountTrie) copy() *accountTrie {
	return &accountTrie{b: t.b, parent: t.parent, w: t.w.clone(), committed: t.committed, root: t.root}
}

func (*accountTrie) GetKey([]byte) []byte { return nil }

func (t *accountTrie) GetAccount(a common.Address) (*types.StateAccount, error) {
	if x, ok := t.w.accts[a]; ok {
		if x == nil {
			return nil, nil
		}
		return x.Copy(), nil
	}
	return t.b.account(t.parent, a)
}

func (t *accountTrie) GetStorage(a common.Address, key []byte) ([]byte, error) {
	k := common.BytesToHash(key)
	if v, ok := t.w.slots[a][k]; ok {
		return common.TrimLeftZeroes(v[:]), nil
	}
	if t.w.deleted[a] {
		return nil, nil
	}
	v, err := t.b.slot(t.parent, a, k)
	if err != nil {
		return nil, err
	}
	return common.TrimLeftZeroes(v[:]), nil
}

func (t *accountTrie) UpdateAccount(a common.Address, acc *types.StateAccount) error {
	t.w.accts[a] = acc.Copy()
	return nil
}

func (t *accountTrie) UpdateStorage(a common.Address, key, value []byte) error {
	m := t.w.slots[a]
	if m == nil {
		m = map[common.Hash]common.Hash{}
		t.w.slots[a] = m
	}
	m[common.BytesToHash(key)] = common.BytesToHash(value)
	return nil
}

func (t *accountTrie) DeleteAccount(a common.Address) error {
	t.w.accts[a] = nil
	t.w.deleted[a] = true
	delete(t.w.slots, a)
	return nil
}

func (t *accountTrie) DeleteStorage(a common.Address, key []byte) error {
	return t.UpdateStorage(a, key, nil)
}

// UpdateContractCode keeps the code for a store that records it (ours, at
// genesis); the code itself lives in the rawdb as for hashdb.
func (t *accountTrie) UpdateContractCode(_ common.Address, h common.Hash, code []byte) error {
	t.w.code[h] = bytes.Clone(code)
	return nil
}

// Hash returns the root of the trie's writes on its parent without changing
// the store. A store that only learns a root by applying the writes (ours)
// returns the zero hash until Commit; [state.StateDB.Commit] ignores the
// result of the Hash it calls, and SAE has no other consumer on the
// execution path.
func (t *accountTrie) Hash() common.Hash {
	if t.committed {
		return t.root
	}
	if t.w.empty() {
		return t.parent
	}
	root, ok, err := t.b.hash(t.parent, t.w)
	if err != nil {
		t.b.logger().Error("hashing account trie", zap.Error(err))
		return common.Hash{}
	}
	if !ok {
		return common.Hash{}
	}
	return root
}

// Commit applies the writes to the store and returns the new root. The store
// keeps no nodes for the [triedb.Database], so the node set is nil.
func (t *accountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	if t.committed {
		return t.root, nil, nil
	}
	if t.w.empty() {
		return t.parent, nil, nil
	}
	root, err := t.b.commit(t.parent, t.w)
	if err != nil {
		return common.Hash{}, nil, err
	}
	t.committed, t.root = true, root
	return root, nil, nil
}

var (
	errNodeIterator = errors.New("plantstore: NodeIterator not supported")
	errProve        = errors.New("plantstore: Prove not supported")
)

func (*accountTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errNodeIterator
}

func (*accountTrie) Prove([]byte, ethdb.KeyValueWriter) error { return errProve }

// storageTrie is an account's storage view of the block's [accountTrie]: the
// writes go to the account trie, which commits them.
type storageTrie struct{ *accountTrie }

// Hash returns the empty root: the stores ignore an account's storage root.
func (storageTrie) Hash() common.Hash { return types.EmptyRootHash }

func (storageTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return types.EmptyRootHash, nil, nil
}

var _ firewood.StateWrapper = (*NullDB)(nil)
var _ firewood.StateWrapper = (*OursDB)(nil)
