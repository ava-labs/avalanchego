// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
)

var _ triedb.DBOverride = (*ReconstructedTrieDB)(nil)

// NewReconstructedTrieDB returns a trie database whose state trie operations are
// backed by recon. The database is intended for temporary reconstruction only:
// trie updates mutate recon, but trie database commits do not persist revisions
// to Firewood.
//
// If computeRootOnHash is false, reconstructed trie Hash and Commit calls apply
// pending writes but return the cached root without forcing an expensive
// reconstructed root computation. This mode is intended for replay paths that
// only need Hash as a state-flush point and validate the final root separately.
func NewReconstructedTrieDB(source *TrieDB, recon *ffi.Reconstructed, computeRootOnHash bool) *triedb.Database {
	return triedb.NewDatabase(rawdb.NewMemoryDatabase(), &triedb.Config{
		DBOverride: func(ethdb.Database) triedb.DBOverride {
			return &ReconstructedTrieDB{
				source:            source,
				recon:             recon,
				computeRootOnHash: computeRootOnHash,
			}
		},
	})
}

// ReconstructedTrieDB is the trie database backend created by
// [NewReconstructedTrieDB]. It is exported only so that state database
// constructors can detect it via [triedb.Database.Backend] and wrap the
// database with [NewReconstructedStateAccessor]; it is not intended to be
// constructed directly.
type ReconstructedTrieDB struct {
	source            *TrieDB
	recon             *ffi.Reconstructed
	computeRootOnHash bool
}

func (r *ReconstructedTrieDB) Scheme() string {
	return r.source.Scheme()
}

func (r *ReconstructedTrieDB) Initialized(genesisRoot common.Hash) bool {
	return common.Hash(r.recon.Root()) == genesisRoot
}

func (*ReconstructedTrieDB) Size() (common.StorageSize, common.StorageSize) {
	return 0, 0
}

func (r *ReconstructedTrieDB) Update(root, _ common.Hash, _ uint64, _ *trienode.MergedNodeSet, _ *triestate.Set, _ ...stateconf.TrieDBUpdateOption) error {
	return r.validateRoot(root)
}

func (r *ReconstructedTrieDB) Commit(root common.Hash, _ bool) error {
	return r.validateRoot(root)
}

func (*ReconstructedTrieDB) Close() error {
	return nil
}

func (*ReconstructedTrieDB) Cap(common.StorageSize) error {
	return nil
}

func (*ReconstructedTrieDB) Reference(common.Hash, common.Hash) {}

func (*ReconstructedTrieDB) Dereference(common.Hash) {}

func (r *ReconstructedTrieDB) Reader(root common.Hash) (database.Reader, error) {
	if err := r.validateRoot(root); err != nil {
		return nil, err
	}
	return &reconstructedReader{reconstructed: r.recon}, nil
}

func (r *ReconstructedTrieDB) validateRoot(root common.Hash) error {
	// In replay mode, the reconstructed trie intentionally reports a stale
	// (cached) root while recon advances, so the caller's root will not match
	// recon.Root(). The final root is validated once after replay instead.
	if !r.computeRootOnHash {
		return nil
	}

	if current := common.Hash(r.recon.Root()); current != root {
		return fmt.Errorf("expected reconstructed root %s but got %s", current.Hex(), root.Hex())
	}
	return nil
}
