// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plantstore

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/dist"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/store"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/tier0"

	etrie "github.com/ava-labs/avalanchego/vms/saevm/oursengine/trie"
)

// Tier0Blocks is how many engine blocks one tier0 takes: the open tier0 ends
// (Top.EndTier0: the re-cuts and the memo sweep) after every engine block
// whose number is a multiple of it. A constant, so every node of a network
// ends its tier0s at the same blocks.
const Tier0Blocks = 64

var (
	_ triedb.HashDB     = (*OursDB)(nil)
	_ triedb.DBOverride = (*OursDB)(nil)
	_ triedb.DBOverride = (*NullDB)(nil)

	// ErrWipedHistory is returned for a slot read at an older root when a
	// later block deleted the slot's account: the ours store keeps the old
	// values of the keys a block wrote, but a deletion drops the account's
	// whole storage without naming its keys. Since Cancun, SELFDESTRUCT and
	// EIP-161 only delete accounts without stored storage, so a chain does
	// not meet it.
	ErrWipedHistory = errors.New("plantstore: slot of an account deleted since the root is not kept")
)

// OursBackend returns the constructor of the ours store kept under dir. The
// first open in a process empties dir: the store holds no state across
// restarts.
func OursBackend(dir string, log logging.Logger) triedb.DBConstructor {
	if log == nil {
		log = logging.NoLog{}
	}
	return func(ethdb.Database) triedb.DBOverride {
		db, err := shared(dir, func() (*OursDB, error) {
			if err := os.RemoveAll(dir); err != nil {
				return nil, err
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
			log.Info("OURS STORE: faster-firewood-node engine, benchmark use only", zap.String("dir", dir))
			return &OursDB{
				dir:        dir,
				log:        log,
				fiveFields: fiveFieldAccounts(),
				tip:        types.EmptyRootHash,
				stored:     map[common.Address]bool{},
			}, nil
		})
		if err != nil {
			log.Fatal("opening the ours store", zap.String("dir", dir), zap.Error(err))
			panic(err)
		}
		return db
	}
}

// fiveFieldAccounts reports whether the registered account RLP has the coreth
// multicoin field.
func fiveFieldAccounts() bool {
	b, err := rlp.EncodeToBytes(&types.StateAccount{Balance: new(uint256.Int), Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash[:]})
	if err != nil {
		return false
	}
	content, _, err := rlp.SplitList(b)
	if err != nil {
		return false
	}
	n, err := rlp.CountValues(content)
	return err == nil && n == 5
}

// multicoin is the coreth multicoin flag of acc (the fifth RLP field).
func multicoin(acc *types.StateAccount) (bool, error) {
	b, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return false, err
	}
	content, _, err := rlp.SplitList(b)
	if err != nil {
		return false, err
	}
	for range 4 {
		if _, _, content, err = rlp.Split(content); err != nil {
			return false, err
		}
	}
	if len(content) == 0 {
		return false, nil
	}
	_, v, _, err := rlp.Split(content)
	return len(v) == 1 && v[0] == 1, err
}

type oursKey struct {
	addr common.Address
	kind byte
	slot common.Hash
}

// oursBlock is one applied block: its root and, for the reads at older roots,
// the value before it of every key it wrote, and the accounts it deleted.
type oursBlock struct {
	root  common.Hash
	old   map[oursKey][32]byte
	wiped map[common.Address]bool

	stores []common.Address // accounts given a non-zero slot (for OursDB.stored)
}

// OursDB is the ours store: the engine's trie top over a base built from the
// genesis state, with a live overlay that takes every block.
//
// TODO: the hourly fold (Top.Swap: the live overlay frozen and drained to a
// level 1 file) is not run, so the live overlay grows for the life of the
// process.
type OursDB struct {
	mu         sync.RWMutex
	dir        string
	log        logging.Logger
	fiveFields bool

	sd  *dist.Dir
	st  *store.State
	top *etrie.Top
	n   uint64 // the engine's next block number

	tip  common.Hash
	hist []*oursBlock // the newest Window+1 states, oldest first; the last is tip
	at   map[common.Hash]int
	// stored holds every account that may have storage (a non-zero slot
	// written since genesis or its deletion), so that the deletion of an
	// account without storage keeps its history readable.
	stored map[common.Address]bool
}

func (d *OursDB) WrapState(db state.Database) state.Database {
	return &accessor{Database: db, b: d}
}

func (d *OursDB) logger() logging.Logger { return d.log }

func (d *OursDB) known(root common.Hash) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.top == nil {
		return root == types.EmptyRootHash
	}
	_, ok := d.at[root]
	return ok
}

// value is key k at root: the value before the oldest block above root that
// wrote k, else the newest state. The caller holds mu.
func (d *OursDB) value(root common.Hash, k oursKey) ([32]byte, error) {
	if d.top == nil {
		if root != types.EmptyRootHash {
			return [32]byte{}, &trie.MissingNodeError{NodeHash: root}
		}
		return [32]byte{}, nil
	}
	i, ok := d.at[root]
	if !ok {
		return [32]byte{}, &trie.MissingNodeError{NodeHash: root}
	}
	for _, b := range d.hist[i+1:] {
		if v, ok := b.old[k]; ok {
			return v, nil
		}
		if k.kind == tier0.KindSlot && b.wiped[k.addr] {
			return [32]byte{}, fmt.Errorf("%w: %s at %s", ErrWipedHistory, k.addr, root)
		}
	}
	return d.st.Value(k.addr, k.kind, k.slot)
}

func (d *OursDB) account(root common.Hash, a common.Address) (*types.StateAccount, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	c, err := d.value(root, oursKey{a, tier0.KindCode, common.Hash{}})
	if err != nil || c == ([32]byte{}) {
		return nil, err
	}
	b, err := d.value(root, oursKey{a, tier0.KindBalance, common.Hash{}})
	if err != nil {
		return nil, err
	}
	n, err := d.value(root, oursKey{a, tier0.KindNonce, common.Hash{}})
	if err != nil {
		return nil, err
	}
	var m [32]byte
	if d.fiveFields {
		if m, err = d.value(root, oursKey{a, tier0.KindMulti, common.Hash{}}); err != nil {
			return nil, err
		}
	}
	// The account RLP as the engine hashes it, decoded so that any registered
	// account extra (the coreth multicoin flag) is set.
	emptyRoot := [32]byte(types.EmptyRootHash)
	enc := etrie.AccountVal(nil, binary.BigEndian.Uint64(n[24:]), b[:], &emptyRoot, c[:], m != ([32]byte{}), d.fiveFields)
	acc := new(types.StateAccount)
	if err := rlp.DecodeBytes(enc, acc); err != nil {
		return nil, err
	}
	return acc, nil
}

func (d *OursDB) slot(root common.Hash, a common.Address, k common.Hash) (common.Hash, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.value(root, oursKey{a, tier0.KindSlot, k})
}

// hash cannot know a root without applying the writes.
func (*OursDB) hash(common.Hash, *writes) (common.Hash, bool, error) {
	return common.Hash{}, false, nil
}

func (d *OursDB) commit(parent common.Hash, w *writes) (common.Hash, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if parent != d.tip {
		return common.Hash{}, fmt.Errorf("%w: parent %s, newest %s", errNotTip, parent, d.tip)
	}
	if d.top == nil {
		return d.openGenesis(w)
	}
	groups, blk, err := d.groups(w)
	if err != nil {
		return common.Hash{}, err
	}
	if len(groups) == 0 { // no change: the parent's state and root
		return parent, nil
	}
	n := d.n
	bs, err := d.top.ApplyBlock(n, groups, etrie.Header{Number: n, Settles: n - 1, Root: d.tip})
	if err != nil {
		return common.Hash{}, fmt.Errorf("ours store: apply block %d: %w", n, err)
	}
	d.n++
	if n%Tier0Blocks == 0 {
		d.top.EndTier0()
	}
	blk.root = common.Hash(bs.RootHash)
	for a := range blk.wiped {
		delete(d.stored, a)
	}
	for _, a := range blk.stores {
		d.stored[a] = true
	}
	blk.stores = nil
	d.push(blk)
	return blk.root, nil
}

// push makes blk the newest state and drops the states beyond the window.
// The caller holds mu.
func (d *OursDB) push(blk *oursBlock) {
	d.hist = append(d.hist, blk)
	if len(d.hist) > Window+1 {
		d.hist[0] = nil
		d.hist = d.hist[len(d.hist)-Window-1:]
	}
	clear(d.at)
	for i, b := range d.hist {
		d.at[b.root] = i
	}
	d.tip = blk.root
}

// groups turns the writes into the engine's write groups, one per address
// (two for an account deleted and written again), with the newest state's
// values as Old. The caller holds mu.
func (d *OursDB) groups(w *writes) ([][]tier0.Write, *oursBlock, error) {
	blk := &oursBlock{old: map[oursKey][32]byte{}, wiped: map[common.Address]bool{}}
	cur := func(a common.Address, kind byte, slot common.Hash) ([32]byte, error) {
		k := oursKey{a, kind, slot}
		if v, ok := blk.old[k]; ok {
			return v, nil
		}
		v, err := d.st.Value(a, kind, slot)
		if err != nil {
			return v, err
		}
		blk.old[k] = v
		return v, nil
	}
	fields := []byte{tier0.KindBalance, tier0.KindNonce, tier0.KindCode}
	if d.fiveFields {
		fields = append(fields, tier0.KindMulti)
	}

	var groups [][]tier0.Write
	for _, a := range w.addrs() {
		acct, touched := w.accts[a]
		wiped := false
		if w.deleted[a] {
			oc, err := cur(a, tier0.KindCode, common.Hash{})
			if err != nil {
				return nil, nil, err
			}
			if oc != ([32]byte{}) {
				// A code hash write to 0 drops the account and its whole
				// storage; the other fields go to 0 with it.
				var g []tier0.Write
				for _, kind := range fields {
					o, err := cur(a, kind, common.Hash{})
					if err != nil {
						return nil, nil, err
					}
					if o != ([32]byte{}) {
						g = append(g, tier0.Write{Addr: a, Kind: kind, Old: o})
					}
				}
				groups = append(groups, g)
				blk.wiped[a] = d.stored[a]
				wiped = true
			}
		}
		if touched && acct == nil {
			continue // deleted (above, if it existed)
		}

		var g []tier0.Write
		for _, k := range sortedKeys(w.slots[a]) {
			o, err := cur(a, tier0.KindSlot, k)
			if err != nil {
				return nil, nil, err
			}
			if wiped {
				o = [32]byte{}
			}
			if v := w.slots[a][k]; o != v {
				g = append(g, tier0.Write{Addr: a, Kind: tier0.KindSlot, Slot: k, Old: o, New: v})
				if v != ([32]byte{}) {
					blk.stores = append(blk.stores, a)
				}
			}
		}
		if touched {
			var nb, nn, nc, nm [32]byte
			nb = acct.Balance.Bytes32()
			binary.BigEndian.PutUint64(nn[24:], acct.Nonce)
			nc = common.BytesToHash(acct.CodeHash)
			if nc == ([32]byte{}) {
				nc = etrie.EmptyCode
			}
			if d.fiveFields {
				mc, err := multicoin(acct)
				if err != nil {
					return nil, nil, err
				}
				if mc {
					nm[31] = 1
				}
			}
			for _, kind := range fields {
				o, err := cur(a, kind, common.Hash{})
				if err != nil {
					return nil, nil, err
				}
				if wiped {
					o = [32]byte{}
				}
				var v [32]byte
				switch kind {
				case tier0.KindBalance:
					v = nb
				case tier0.KindNonce:
					v = nn
				case tier0.KindCode:
					v = nc
				case tier0.KindMulti:
					v = nm
				}
				if o != v {
					g = append(g, tier0.Write{Addr: a, Kind: kind, Old: o, New: v})
				}
			}
		}
		if len(g) > 0 {
			groups = append(groups, g)
		}
	}
	return groups, blk, nil
}

// openGenesis builds the engine's base from the genesis writes (accounts by
// key hash, their slots by key hash, then the code records) and the trie top
// over it, whose root is the genesis root. The caller holds mu.
func (d *OursDB) openGenesis(w *writes) (common.Hash, error) {
	type acct struct {
		ah    common.Hash
		a     *types.StateAccount
		slots [][2]common.Hash
	}
	var accts []acct
	for a, x := range w.accts {
		if x == nil {
			continue
		}
		e := acct{ah: crypto.Keccak256Hash(a[:]), a: x}
		for k, v := range w.slots[a] {
			if v != (common.Hash{}) {
				e.slots = append(e.slots, [2]common.Hash{crypto.Keccak256Hash(k[:]), v})
			}
		}
		slices.SortFunc(e.slots, func(x, y [2]common.Hash) int { return bytes.Compare(x[0][:], y[0][:]) })
		if len(e.slots) > 0 {
			d.stored[a] = true
		}
		accts = append(accts, e)
	}
	slices.SortFunc(accts, func(x, y acct) int { return bytes.Compare(x.ah[:], y.ah[:]) })

	ew := era.NewWriter(0)
	for _, e := range accts {
		var ch []byte
		if c := common.BytesToHash(e.a.CodeHash); c != (common.Hash{}) && c != common.Hash(etrie.EmptyCode) {
			ch = c[:]
		}
		mc := false
		if d.fiveFields {
			var err error
			if mc, err = multicoin(e.a); err != nil {
				return common.Hash{}, err
			}
		}
		bal := e.a.Balance.Bytes32()
		ew.Account(e.ah, e.a.Nonce, bal[:], ch, mc)
		for _, kv := range e.slots {
			ew.Slot(e.ah, kv[0], kv[1][:])
		}
	}
	var chs []common.Hash
	for h := range w.code {
		if h != common.Hash(etrie.EmptyCode) {
			chs = append(chs, h)
		}
	}
	slices.SortFunc(chs, func(x, y common.Hash) int { return bytes.Compare(x[:], y[:]) })
	for _, h := range chs {
		ew.Code(h, w.code[h])
	}

	tmp := filepath.Join(d.dir, "genesis.base.tmp")
	cw, err := chunkfile.Create(tmp)
	if err != nil {
		return common.Hash{}, err
	}
	if _, err := cw.Write(ew.Finish()); err != nil {
		return common.Hash{}, err
	}
	fh, err := cw.Finish()
	if err != nil {
		return common.Hash{}, err
	}
	p := filepath.Join(d.dir, hex.EncodeToString(fh[:])+".base")
	if err := os.Rename(tmp, p); err != nil {
		return common.Hash{}, err
	}
	sd, err := dist.OpenDir(d.dir)
	if err != nil {
		return common.Hash{}, err
	}
	cf, err := sd.File(fh)
	if err != nil {
		return common.Hash{}, errors.Join(err, sd.Close())
	}
	bs, err := store.OpenBase(cf, p)
	if err != nil {
		return common.Hash{}, errors.Join(err, sd.Close())
	}
	eng := &etrie.Engine{K: etrie.DefaultK, Threads: runtime.GOMAXPROCS(0), FiveFields: d.fiveFields}
	root, err := eng.Build(bs)
	if err != nil {
		return common.Hash{}, errors.Join(fmt.Errorf("ours store: build the genesis top: %w", err), sd.Close())
	}
	top := &etrie.Top{E: eng, Block: bs.Block(), Root: root}
	st := store.New(bs)
	if err := top.ReadFrom(st); err != nil {
		return common.Hash{}, errors.Join(err, sd.Close())
	}
	top.Live = &etrie.Live{}
	top.LiveOverlay = true
	st.SetUpper(top)

	d.sd, d.st, d.top, d.n = sd, st, top, bs.Block()+1
	d.at = map[common.Hash]int{}
	d.push(&oursBlock{root: common.Hash(root)})
	d.log.Info("ours store: genesis base built",
		zap.Int("accounts", len(accts)),
		zap.Stringer("root", d.tip),
	)
	return d.tip, nil
}

// Scheme returns [rawdb.HashScheme] so that [state.StateDB.Commit] does not
// iterate a destructed account's storage (as for Firewood).
func (*OursDB) Scheme() string { return rawdb.HashScheme }

func (d *OursDB) Initialized(genesisRoot common.Hash) bool {
	if genesisRoot == types.EmptyRootHash {
		return true
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.top != nil
}

func (*OursDB) Size() (common.StorageSize, common.StorageSize) { return 0, 0 }
func (*OursDB) Cap(common.StorageSize) error                   { return nil }
func (*OursDB) Reference(common.Hash, common.Hash)             {}
func (*OursDB) Dereference(common.Hash)                        {}

// Close keeps the state: the store lives as long as the process (see
// [Release]).
func (*OursDB) Close() error { return nil }

func (d *OursDB) release() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.top == nil {
		return nil
	}
	err := errors.Join(d.top.Close(), d.sd.Close())
	d.top, d.st, d.sd = nil, nil, nil
	return err
}

// Update checks that root is the newest state (applied by Commit of the
// block's account trie).
//
//nolint:revive // removing names loses context.
func (d *OursDB) Update(root, parent common.Hash, block uint64, _ *trienode.MergedNodeSet, _ *triestate.Set, _ ...stateconf.TrieDBUpdateOption) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if root != d.tip {
		return fmt.Errorf("%w: update %s at block %d (parent %s), newest %s", errNotUpdated, root, block, parent, d.tip)
	}
	return nil
}

// Commit has nothing to persist.
func (*OursDB) Commit(common.Hash, bool) error { return nil }

// Reader is not supported: the store serves no trie nodes.
func (*OursDB) Reader(common.Hash) (database.Reader, error) { return nil, errNoReader }
