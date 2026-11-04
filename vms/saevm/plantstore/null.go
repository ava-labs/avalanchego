// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plantstore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ triedb.HashDB = (*NullDB)(nil)

// NullBackend returns the constructor of the null store kept under dir.
func NullBackend(dir string, log logging.Logger) triedb.DBConstructor {
	if log == nil {
		log = logging.NoLog{}
	}
	return func(ethdb.Database) triedb.DBOverride {
		db, _ := shared(dir, func() (*NullDB, error) {
			log.Info("NULL STORE: state in Go maps, fake roots, benchmark use only", zap.String("dir", dir))
			return newNullDB(log), nil
		})
		return db
	}
}

// NullDB keeps the newest state in flat Go maps, written in place at commit, plus one undo
// record (the old values of what the block wrote) per block for reads at the [Window] older
// roots. A read at the newest root is one map lookup.
type NullDB struct {
	mu        sync.RWMutex
	log       logging.Logger
	tipRoot   common.Hash
	tipHeight uint64
	accounts  map[common.Address]*types.StateAccount
	storage   map[nullSlot]common.Hash  // key: address, incarnation, slot
	inc       map[common.Address]uint32 // an account's storage incarnation (a wipe starts a new one)
	undo      []*nullUndo               // oldest first; the last is the newest block's
	heights   map[common.Hash]uint64    // every readable root
}

type nullSlot struct {
	a   common.Address
	inc uint32
	k   common.Hash
}

// nullUndo is one block's old values (an absent account is a nil pointer).
type nullUndo struct {
	root   common.Hash
	height uint64
	accts  map[common.Address]*types.StateAccount
	slots  map[nullSlot]common.Hash
	incs   map[common.Address]uint32
}

func newNullDB(log logging.Logger) *NullDB {
	return &NullDB{
		log:      log,
		tipRoot:  types.EmptyRootHash,
		accounts: map[common.Address]*types.StateAccount{},
		storage:  map[nullSlot]common.Hash{},
		inc:      map[common.Address]uint32{},
		heights:  map[common.Hash]uint64{types.EmptyRootHash: 0},
	}
}

func (n *NullDB) WrapState(db state.Database) state.Database {
	return &accessor{Database: db, b: n}
}

func (n *NullDB) logger() logging.Logger { return n.log }

func (n *NullDB) tip() common.Hash { return n.tipRoot }

func (n *NullDB) known(root common.Hash) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	_, ok := n.heights[root]
	return ok
}

// height is root's height. The caller holds mu.
func (n *NullDB) height(root common.Hash) (uint64, error) {
	h, ok := n.heights[root]
	if !ok {
		return 0, &trie.MissingNodeError{NodeHash: root}
	}
	return h, nil
}

func (n *NullDB) account(root common.Hash, a common.Address) (*types.StateAccount, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	h, err := n.height(root)
	if err != nil {
		return nil, err
	}
	x := n.accounts[a]
	for i := len(n.undo) - 1; i >= 0 && n.undo[i].height > h; i-- { // older roots only
		if old, ok := n.undo[i].accts[a]; ok {
			x = old
		}
	}
	if x == nil {
		return nil, nil
	}
	return x.Copy(), nil
}

func (n *NullDB) slot(root common.Hash, a common.Address, k common.Hash) (common.Hash, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	h, err := n.height(root)
	if err != nil {
		return common.Hash{}, err
	}
	if h == n.tipHeight {
		return n.storage[nullSlot{a, n.inc[a], k}], nil
	}
	inc := n.inc[a]
	for i := len(n.undo) - 1; i >= 0 && n.undo[i].height > h; i-- {
		if old, ok := n.undo[i].incs[a]; ok {
			inc = old
		}
	}
	key := nullSlot{a, inc, k}
	v := n.storage[key]
	for i := len(n.undo) - 1; i >= 0 && n.undo[i].height > h; i-- {
		if old, ok := n.undo[i].slots[key]; ok {
			v = old
		}
	}
	return v, nil
}

// rootOn is the root of w on parent. The caller holds mu.
func (n *NullDB) rootOn(parent common.Hash, w *writes) (common.Hash, uint64, error) {
	h, ok := n.heights[parent]
	if !ok {
		return common.Hash{}, 0, &trie.MissingNodeError{NodeHash: parent}
	}
	if parent == types.EmptyRootHash && len(n.undo) == 0 && len(n.accounts) == 0 {
		// Genesis keeps its real Merkle root: the VM computes it with hashdb.
		root, err := genesisRoot(w)
		return root, h + 1, err
	}
	var buf [common.HashLength + 8]byte
	copy(buf[:], parent[:])
	binary.BigEndian.PutUint64(buf[common.HashLength:], h+1)
	return crypto.Keccak256Hash(buf[:]), h + 1, nil
}

func (n *NullDB) hash(parent common.Hash, w *writes) (common.Hash, bool, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	root, _, err := n.rootOn(parent, w)
	return root, err == nil, err
}

func (n *NullDB) commit(parent common.Hash, w *writes) (common.Hash, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if tip := n.tip(); parent != tip {
		return common.Hash{}, fmt.Errorf("%w: parent %s, newest %s", errNotTip, parent, tip)
	}
	root, h, err := n.rootOn(parent, w)
	if err != nil {
		return common.Hash{}, err
	}
	u := &nullUndo{root: root, height: h, accts: map[common.Address]*types.StateAccount{}, slots: map[nullSlot]common.Hash{}, incs: map[common.Address]uint32{}}
	setAcct := func(a common.Address, x *types.StateAccount) {
		if _, ok := u.accts[a]; !ok {
			u.accts[a] = n.accounts[a]
		}
		if x == nil {
			delete(n.accounts, a)
		} else {
			n.accounts[a] = x.Copy()
		}
	}
	wipe := func(a common.Address) { // a new storage incarnation: the old slots stay, unread
		if _, ok := u.incs[a]; !ok {
			u.incs[a] = n.inc[a]
		}
		n.inc[a]++
	}
	for a := range w.deleted {
		wipe(a)
	}
	for a, x := range w.accts {
		if x == nil {
			wipe(a)
		}
		setAcct(a, x)
	}
	for a, m := range w.slots {
		inc := n.inc[a]
		for k, v := range m {
			key := nullSlot{a, inc, k}
			if _, ok := u.slots[key]; !ok {
				u.slots[key] = n.storage[key]
			}
			if v == (common.Hash{}) {
				delete(n.storage, key)
			} else {
				n.storage[key] = v
			}
		}
	}
	n.undo = append(n.undo, u)
	n.heights[root] = h
	n.tipRoot, n.tipHeight = root, h
	for len(n.undo) > Window { // the oldest readable root goes
		old := n.undo[0]
		n.undo[0] = nil
		n.undo = n.undo[1:]
		for r, x := range n.heights {
			if x < old.height {
				delete(n.heights, r)
			}
		}
	}
	return root, nil
}

// genesisRoot is the Merkle root of w on an empty state.
func genesisRoot(w *writes) (common.Hash, error) {
	type leaf struct {
		key common.Hash
		val []byte
	}
	var accts []leaf
	for a, x := range w.accts {
		if x == nil {
			continue
		}
		var slots []leaf
		for k, v := range w.slots[a] {
			if v == (common.Hash{}) {
				continue
			}
			enc, err := rlp.EncodeToBytes(common.TrimLeftZeroes(v[:]))
			if err != nil {
				return common.Hash{}, err
			}
			slots = append(slots, leaf{crypto.Keccak256Hash(k[:]), enc})
		}
		slices.SortFunc(slots, func(x, y leaf) int { return bytes.Compare(x.key[:], y.key[:]) })
		st := trie.NewStackTrie(nil)
		for _, s := range slots {
			if err := st.Update(s.key[:], s.val); err != nil {
				return common.Hash{}, err
			}
		}
		acc := x.Copy()
		acc.Root = st.Hash()
		enc, err := rlp.EncodeToBytes(acc)
		if err != nil {
			return common.Hash{}, err
		}
		accts = append(accts, leaf{crypto.Keccak256Hash(a[:]), enc})
	}
	slices.SortFunc(accts, func(x, y leaf) int { return bytes.Compare(x.key[:], y.key[:]) })
	at := trie.NewStackTrie(nil)
	for _, l := range accts {
		if err := at.Update(l.key[:], l.val); err != nil {
			return common.Hash{}, err
		}
	}
	return at.Hash(), nil
}

// Scheme returns [rawdb.HashScheme] so that [state.StateDB.Commit] does not
// iterate a destructed account's storage (as for Firewood).
func (*NullDB) Scheme() string { return rawdb.HashScheme }

func (n *NullDB) Initialized(genesisRoot common.Hash) bool {
	if genesisRoot == types.EmptyRootHash {
		return true
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.tip() != types.EmptyRootHash
}

func (*NullDB) Size() (common.StorageSize, common.StorageSize) { return 0, 0 }
func (*NullDB) Cap(common.StorageSize) error                   { return nil }
func (*NullDB) Reference(common.Hash, common.Hash)             {}
func (*NullDB) Dereference(common.Hash)                        {}

// Close keeps the state: the store lives as long as the process.
func (*NullDB) Close() error { return nil }

// Update checks that root is the newest state (written by Commit of the
// block's account trie).
//
//nolint:revive // removing names loses context.
func (n *NullDB) Update(root, parent common.Hash, block uint64, _ *trienode.MergedNodeSet, _ *triestate.Set, _ ...stateconf.TrieDBUpdateOption) error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if tip := n.tip(); root != tip {
		return fmt.Errorf("%w: update %s at block %d (parent %s), newest %s", errNotUpdated, root, block, parent, tip)
	}
	return nil
}

// Commit has nothing to persist.
func (*NullDB) Commit(common.Hash, bool) error { return nil }

var errNoReader = errors.New("plantstore: no trie node reader")

// Reader is not supported: the store has no trie nodes.
func (*NullDB) Reader(common.Hash) (database.Reader, error) { return nil, errNoReader }
