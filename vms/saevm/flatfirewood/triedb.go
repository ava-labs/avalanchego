// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package flatfirewood implements the "flatfirewood" state scheme: a pruned
// Firewood database wrapped so that every block's state changes are also
// written as flat (hashed key, block) -> value rows to a sibling database,
// from which the state at any historical root can be read back without
// re-execution and without retaining historical trie nodes.
//
// Rows for a block are written in [TrieDB.Update], before the Firewood
// proposal can be committed, so history is never behind the durable Firewood
// state. Crash recovery re-executes from the last committed state and rewrites
// identical rows. A block that leaves the root unchanged never reaches Update
// and needs no rows: its root already maps to an earlier height with the same
// state.
package flatfirewood

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/vms/saevm/firewood"
)

const (
	// Scheme identifies this state scheme in configuration, alongside
	// [rawdb.HashScheme] and [customrawdb.FirewoodScheme].
	Scheme = "flatfirewood"
	// Directory is the name of the history database directory, created next
	// to the Firewood database under the chain data directory.
	Directory = "flatfirewood"
)

// ErrNoBaseline is returned when the history does not cover the chain from
// genesis: the Firewood database already holds state that the history has no
// rows for.
var ErrNoBaseline = errors.New("flatfirewood: history has no rows for the committed state; resync from genesis")

// Config configures a [TrieDB].
type Config struct {
	// Firewood configures the wrapped Firewood database. Archive MUST be
	// false: an archival Firewood database already retains every revision.
	Firewood firewood.Config
	// Path is the directory of the history database.
	Path string
}

// BackendConstructor can be supplied as a [triedb.DBConstructor]. Errors are
// logged as [logging.Fatal], like [firewood.Config.BackendConstructor].
func (c Config) BackendConstructor(diskdb ethdb.Database) triedb.DBOverride {
	db, err := New(diskdb, c)
	if err != nil {
		if c.Firewood.Log == nil {
			panic(fmt.Errorf("creating flatfirewood database: %w", err))
		}
		c.Firewood.Log.Fatal("creating flatfirewood database", zap.Error(err))
	}
	return db
}

var (
	_ triedb.HashDB                     = (*TrieDB)(nil) // libevm requires a hash- or path-like override
	_ firewood.StateDatabaseInterceptor = (*TrieDB)(nil)
)

// TrieDB is a [triedb.DBOverride] wrapping a Firewood [firewood.TrieDB].
type TrieDB struct {
	fw *firewood.TrieDB
	// inner is the Firewood-backed state database whose tries perform the
	// actual reads and proposals; [stateAccessor] wraps them.
	inner state.Database
	store *store
	// pending holds the ops recorded by the last committed account trie until
	// [TrieDB.Update] flushes them.
	pending *recorded
}

type recorded struct {
	root common.Hash
	ops  []op
}

// New opens the Firewood database described by c.Firewood and the history
// database at c.Path. It returns [ErrNoBaseline] if Firewood holds state that
// the history has no rows for.
func New(diskdb ethdb.Database, c Config) (*TrieDB, error) {
	if c.Firewood.Archive {
		return nil, errors.New("flatfirewood: Firewood archive mode is redundant with flat history")
	}
	fwTrieDB := triedb.NewDatabase(diskdb, &triedb.Config{DBOverride: c.Firewood.BackendConstructor})
	fw, ok := fwTrieDB.Backend().(*firewood.TrieDB)
	if !ok {
		return nil, fmt.Errorf("unexpected Firewood backend %T", fwTrieDB.Backend())
	}
	historyDB, err := pebbledb.New(c.Path, nil, c.Firewood.Log, nil)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("opening history database: %w", err), fwTrieDB.Close())
	}
	t := &TrieDB{
		fw:    fw,
		inner: state.NewDatabaseWithNodeDB(diskdb, fwTrieDB),
		store: &store{db: historyDB},
	}
	if root := common.Hash(fw.Firewood.Root()); root != types.EmptyRootHash {
		_, ok, err := t.store.heightOf(root)
		if err == nil && !ok {
			err = fmt.Errorf("%w: Firewood root %#x", ErrNoBaseline, root)
		}
		if err != nil {
			return nil, errors.Join(err, t.Close())
		}
	}
	return t, nil
}

// Update flushes the block's recorded state changes to the history and then
// forwards to Firewood.
func (t *TrieDB) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set, opts ...stateconf.TrieDBUpdateOption) error {
	p := t.pending
	t.pending = nil
	if p == nil || p.root != root {
		return fmt.Errorf("no recorded state changes for root %#x", root)
	}
	if err := t.store.flush(root, block, p.ops); err != nil {
		return fmt.Errorf("flushing history for block %d: %w", block, err)
	}
	return t.fw.Update(root, parent, block, nodes, states, opts...)
}

func (t *TrieDB) Commit(root common.Hash, report bool) error { return t.fw.Commit(root, report) }
func (t *TrieDB) Scheme() string                             { return t.fw.Scheme() }
func (t *TrieDB) Initialized(genesisRoot common.Hash) bool   { return t.fw.Initialized(genesisRoot) }
func (t *TrieDB) Size() (common.StorageSize, common.StorageSize) {
	return t.fw.Size()
}

func (t *TrieDB) Reader(root common.Hash) (database.Reader, error) { return t.fw.Reader(root) }

// Cap, Reference and Dereference make TrieDB a [triedb.HashDB], like the
// wrapped Firewood database.
func (t *TrieDB) Cap(limit common.StorageSize) error { return t.fw.Cap(limit) }
func (t *TrieDB) Reference(root, parent common.Hash) { t.fw.Reference(root, parent) }
func (t *TrieDB) Dereference(root common.Hash)       { t.fw.Dereference(root) }

// Close closes the Firewood and history databases.
func (t *TrieDB) Close() error {
	return errors.Join(t.inner.TrieDB().Close(), t.store.db.Close())
}
