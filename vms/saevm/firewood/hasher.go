// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
)

// hasher serves reads from, and computes the root of, a parent state with the
// trie's pending updates applied. Exactly one hasher backs each [accountTrie].
//
// Not concurrent-safe.
type hasher interface {
	trieReader
	hash(ops []ffi.BatchOp) (common.Hash, error)
	commit() error
	copy() (hasher, error)
}

var (
	_ hasher = (*proposalHasher)(nil)
	_ hasher = (*reconstructedHasher)(nil)
)

// proposalHasher hashes by creating an [ffi.Proposal] on top of a parent that
// is either the Firewood tip or a not-yet-committed proposal tracked by the
// [TrieDB]. Its result can later be committed.
type proposalHasher struct {
	tdb        *TrieDB
	parentRoot common.Hash
	revision   *ffi.Revision

	pending  *ffi.Proposal // MAY be nil
	proposed int           // len(ops) reflected by pending
	root     common.Hash   // root of pending, or parentRoot if pending is nil
}

func newProposalHasher(tdb *TrieDB, parentRoot common.Hash, revision *ffi.Revision) *proposalHasher {
	return &proposalHasher{
		tdb:        tdb,
		parentRoot: parentRoot,
		revision:   revision,
		root:       parentRoot,
	}
}

func (p *proposalHasher) Get(key []byte) ([]byte, error) {
	if p.pending != nil {
		return p.pending.Get(key)
	}
	return p.revision.Get(key)
}

// hash re-proposes the full set of ops on top of the parent whenever ops has
// grown since the last proposal. Any previous proposal is freed once garbage
// collected.
func (p *proposalHasher) hash(ops []ffi.BatchOp) (common.Hash, error) {
	if len(ops) == p.proposed {
		return p.root, nil
	}

	proposal, err := p.tdb.newProposal(p.parentRoot, ops)
	if err != nil {
		return common.Hash{}, err
	}

	p.pending = proposal
	p.proposed = len(ops)
	p.root = common.Hash(proposal.Root())
	return p.root, nil
}

func (p *proposalHasher) commit() error {
	// The [state.StateDB] only calls [triedb.Database.Update] when the root
	// differs from the parent, which is the only case in which there are
	// changes to commit.
	if p.root == p.parentRoot {
		return nil
	}
	return p.tdb.trieCommit(p.pending)
}

// copy returns a hasher that reads from the parent revision and re-proposes
// all ops on its next hash. It MUST NOT share the pending proposal, which
// becomes invalid once the original commits it.
func (p *proposalHasher) copy() (hasher, error) {
	return newProposalHasher(p.tdb, p.parentRoot, p.revision), nil
}

// reconstructedHasher hashes by building an [ffi.Reconstructed] view on top of
// a historical revision that can no longer be proposed on.
type reconstructedHasher struct {
	view    *ffi.Reconstructed
	applied int // len(ops) reflected by view
}

func newReconstructedHasher(recon *ffi.Reconstructed) *reconstructedHasher {
	return &reconstructedHasher{view: recon}
}

func (r *reconstructedHasher) Get(key []byte) ([]byte, error) {
	return r.view.Get(key)
}

func (r *reconstructedHasher) hash(ops []ffi.BatchOp) (common.Hash, error) {
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

func (*reconstructedHasher) commit() error {
	return errHistoricalNotCommittable
}

// copy clones the reconstructed view, if any, so the copy starts from the
// already-hashed state instead of replaying every op.
func (r *reconstructedHasher) copy() (hasher, error) {
	recon, err := r.view.Clone()
	if err != nil {
		return nil, err
	}

	cp := newReconstructedHasher(recon)
	cp.applied = r.applied
	return cp, nil
}
