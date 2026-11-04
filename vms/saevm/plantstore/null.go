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

// NullDB keeps the state in Go maps: a flat base and one layer of writes per
// block above it, at most [Window] layers. The oldest layer is flattened into
// the base when a block makes more.
type NullDB struct {
	mu         sync.RWMutex
	log        logging.Logger
	baseRoot   common.Hash
	baseHeight uint64
	accounts   map[common.Address]*types.StateAccount
	storage    map[common.Address]map[common.Hash]common.Hash
	layers     []*nullLayer           // oldest first; layers[i] is at height baseHeight+i+1
	heights    map[common.Hash]uint64 // every readable root
}

type nullLayer struct {
	root common.Hash
	w    *writes
}

func newNullDB(log logging.Logger) *NullDB {
	return &NullDB{
		log:      log,
		baseRoot: types.EmptyRootHash,
		accounts: map[common.Address]*types.StateAccount{},
		storage:  map[common.Address]map[common.Hash]common.Hash{},
		heights:  map[common.Hash]uint64{types.EmptyRootHash: 0},
	}
}

func (n *NullDB) WrapState(db state.Database) state.Database {
	return &accessor{Database: db, b: n}
}

func (n *NullDB) logger() logging.Logger { return n.log }

func (n *NullDB) tip() common.Hash {
	if len(n.layers) == 0 {
		return n.baseRoot
	}
	return n.layers[len(n.layers)-1].root
}

func (n *NullDB) known(root common.Hash) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	_, ok := n.heights[root]
	return ok
}

// upTo returns the layers from root's own down to the base, newest first.
// The caller holds mu.
func (n *NullDB) upTo(root common.Hash) ([]*nullLayer, error) {
	h, ok := n.heights[root]
	if !ok {
		return nil, &trie.MissingNodeError{NodeHash: root}
	}
	ls := slices.Clone(n.layers[:h-n.baseHeight])
	slices.Reverse(ls)
	return ls, nil
}

func (n *NullDB) account(root common.Hash, a common.Address) (*types.StateAccount, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	ls, err := n.upTo(root)
	if err != nil {
		return nil, err
	}
	for _, l := range ls {
		if x, ok := l.w.accts[a]; ok {
			if x == nil {
				return nil, nil
			}
			return x.Copy(), nil
		}
	}
	if x := n.accounts[a]; x != nil {
		return x.Copy(), nil
	}
	return nil, nil
}

func (n *NullDB) slot(root common.Hash, a common.Address, k common.Hash) (common.Hash, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	ls, err := n.upTo(root)
	if err != nil {
		return common.Hash{}, err
	}
	for _, l := range ls {
		if v, ok := l.w.slots[a][k]; ok {
			return v, nil
		}
		if l.w.deleted[a] {
			return common.Hash{}, nil
		}
	}
	return n.storage[a][k], nil
}

// rootOn is the root of w on parent. The caller holds mu.
func (n *NullDB) rootOn(parent common.Hash, w *writes) (common.Hash, uint64, error) {
	h, ok := n.heights[parent]
	if !ok {
		return common.Hash{}, 0, &trie.MissingNodeError{NodeHash: parent}
	}
	if parent == types.EmptyRootHash && len(n.layers) == 0 && len(n.accounts) == 0 {
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
	n.layers = append(n.layers, &nullLayer{root: root, w: w.clone()})
	n.heights[root] = h
	for len(n.layers) > Window {
		n.flattenOldest()
	}
	return root, nil
}

// flattenOldest moves the oldest layer into the base. The caller holds mu.
func (n *NullDB) flattenOldest() {
	l := n.layers[0]
	n.layers[0] = nil
	n.layers = n.layers[1:]
	for a := range l.w.deleted {
		delete(n.storage, a)
	}
	for a, x := range l.w.accts {
		if x == nil {
			delete(n.accounts, a)
			delete(n.storage, a)
		} else {
			n.accounts[a] = x
		}
	}
	for a, m := range l.w.slots {
		s := n.storage[a]
		if s == nil {
			s = map[common.Hash]common.Hash{}
			n.storage[a] = s
		}
		for k, v := range m {
			if v == (common.Hash{}) {
				delete(s, k)
			} else {
				s[k] = v
			}
		}
		if len(s) == 0 {
			delete(n.storage, a)
		}
	}
	delete(n.heights, n.baseRoot)
	n.baseRoot = l.root
	n.baseHeight++
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
