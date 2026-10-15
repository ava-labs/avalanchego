// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
)

// view serves reads from, and computes the root of, a parent state with the
// trie's pending updates applied. Exactly one view backs each [accountTrie].
//
// Not concurrent-safe.
type view interface {
	trieReader
	hash(ops []ffi.BatchOp) (common.Hash, error)
	commit() error
}

var (
	_ view = (*proposableView)(nil)
	_ view = (*reconstructedView)(nil)
)

// proposableView hashes by creating an [ffi.Proposal] on top of a parent that
// is either the Firewood tip or a not-yet-committed proposal tracked by the
// [TrieDB]. Its result can later be committed.
type proposableView struct {
	tdb      *TrieDB
	revision *ffi.Revision

	pending  *ffi.Proposal // MAY be nil
	proposed int           // len(ops) reflected by pending
	root     common.Hash
}

func newProposableView(tdb *TrieDB, revision *ffi.Revision) *proposableView {
	return &proposableView{
		tdb:      tdb,
		revision: revision,
		root:     common.Hash(revision.Root()),
	}
}

func (p *proposableView) get(key []byte) ([]byte, error) {
	if p.pending != nil {
		return p.pending.Get(key)
	}
	return p.revision.Get(key)
}

// hash re-proposes the full set of ops on top of the parent whenever ops has
// grown since the last proposal. Any previous proposal is freed once garbage
// collected.
func (p *proposableView) hash(ops []ffi.BatchOp) (common.Hash, error) {
	if len(ops) == p.proposed {
		return p.root, nil
	}

	proposal, err := p.tdb.newProposal(common.Hash(p.revision.Root()), ops)
	if err != nil {
		return common.Hash{}, err
	}

	p.pending = proposal
	p.proposed = len(ops)
	p.root = common.Hash(proposal.Root())
	return p.root, nil
}

func (p *proposableView) commit() error {
	// The [state.StateDB] only calls [triedb.Database.Update] when the root
	// differs from the parent, which is the only case in which there are
	// changes to commit.
	if p.root == common.Hash(p.revision.Root()) {
		return nil
	}
	return p.tdb.trieCommit(p.pending)
}

// reconstructedView hashes by building an [ffi.Reconstructed] view on top of
// a historical revision that can no longer be proposed on.
type reconstructedView struct {
	view    *ffi.Reconstructed
	applied int // len(ops) reflected by view
}

func newReconstructedView(recon *ffi.Reconstructed) *reconstructedView {
	return &reconstructedView{view: recon}
}

func (r *reconstructedView) get(key []byte) ([]byte, error) {
	return r.view.Get(key)
}

func (r *reconstructedView) hash(ops []ffi.BatchOp) (common.Hash, error) {
	if len(ops) == r.applied {
		return common.Hash(r.view.Root()), nil
	}

	if err := r.view.Reconstruct(ops[r.applied:]); err != nil {
		return common.Hash{}, err
	}

	r.applied = len(ops)
	return common.Hash(r.view.Root()), nil
}

var errHistoricalNotCommittable = errors.New("state built on a historical revision cannot be committed")

func (*reconstructedView) commit() error {
	return errHistoricalNotCommittable
}
