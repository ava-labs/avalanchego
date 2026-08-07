// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/trie/trienode"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// NewReconstructedDatabase returns a [state.Database] whose tries read and write
// recon, along with a function that releases recon and every clone made by the
// database. db MUST be the canonical Firewood state database. It supplies
// contract code, which lives outside the trie.
func NewReconstructedDatabase(db state.Database, tdb *TrieDB, recon *ffi.Reconstructed, root common.Hash) (state.Database, func()) {
	session := &reconstructedSession{
		views: []*ffi.Reconstructed{recon},
		log:   tdb.log,
	}
	accessor := &reconstructedStateAccessor{
		Database: db,
		recon:    recon,
		root:     &root,
		session:  session,
	}
	return accessor, session.close
}

var _ state.Database = (*reconstructedStateAccessor)(nil)

// errUnexpectedReconstructedRoot means a trie was requested at a root other
// than the one the reconstructed view currently holds.
var errUnexpectedReconstructedRoot = errors.New("root does not match reconstructed view")

// reconstructedStateAccessor is a [state.Database] whose tries read and write an
// [ffi.Reconstructed] rather than Firewood revisions and proposals.
type reconstructedStateAccessor struct {
	state.Database
	recon *ffi.Reconstructed
	// root is shared with every trie opened by this accessor, which advances it
	// as the view is mutated in place.
	root    *common.Hash
	session *reconstructedSession
}

// reconstructedSession owns every native view created for one reconstructed
// database. Calls to clone and close MUST NOT run concurrently.
type reconstructedSession struct {
	views []*ffi.Reconstructed
	log   logging.Logger
}

func (s *reconstructedSession) clone(view *ffi.Reconstructed) (*ffi.Reconstructed, error) {
	clone, err := view.Clone()
	if err != nil {
		return nil, err
	}
	s.views = append(s.views, clone)
	return clone, nil
}

func (s *reconstructedSession) close() {
	views := s.views
	s.views = nil

	for _, view := range views {
		if err := view.Drop(); err != nil {
			s.log.Warn("dropping reconstructed view", zap.Error(err))
		}
	}
}

// OpenTrie opens the main account trie.
//
// A reconstructed view holds exactly one state, which advances as blocks are
// replayed onto it, so only its current root is accepted. Any other root returns
// [errUnexpectedReconstructedRoot] rather than silently serving the wrong state.
func (s *reconstructedStateAccessor) OpenTrie(root common.Hash) (state.Trie, error) {
	if *s.root != root {
		return nil, fmt.Errorf("%w: view is at %s, requested %s", errUnexpectedReconstructedRoot, *s.root, root)
	}
	return newReconstructedAccountTrie(s.recon, s.root, s.session), nil
}

// OpenStorageTrie opens a wrapped version of the account trie.
//
//nolint:revive // removing names loses context.
func (*reconstructedStateAccessor) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, tr state.Trie) (state.Trie, error) {
	accountTrie, ok := tr.(*reconstructedAccountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type: %T", tr)
	}
	return newStorageTrie(accountTrie.baseTrie), nil
}

// CopyTrie returns a deep copy of the given trie, backed by an independent
// native view. It can be altered by the caller.
func (s *reconstructedStateAccessor) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *reconstructedAccountTrie:
		return t.Copy() // MUST NOT be nil
	case *storageTrie:
		// Returning nil makes [state.StateDB.Copy] reopen the storage tries on
		// top of the copied account trie; see [stateAccessor.CopyTrie].
		return nil
	default:
		s.session.log.Fatal("unknown trie type", zap.String("type", fmt.Sprintf("%T", t)))
		return nil
	}
}

var _ state.Trie = (*reconstructedAccountTrie)(nil)

// reconstructedAccountTrie is the account [state.Trie] for an isolated
// [ffi.Reconstructed] view. Unlike [accountTrie], which builds an [ffi.Proposal]
// per hash, it advances a single mutable view in place. It shares the flat
// account and storage encoding of [baseTrie] and MUST NOT be committed.
//
// Note this is not concurrent safe.
type reconstructedAccountTrie struct {
	*baseTrie
	recon   *ffi.Reconstructed
	root    *common.Hash
	session *reconstructedSession
}

func newReconstructedAccountTrie(recon *ffi.Reconstructed, root *common.Hash, session *reconstructedSession) *reconstructedAccountTrie {
	return &reconstructedAccountTrie{
		baseTrie: &baseTrie{reader: recon},
		recon:    recon,
		root:     root,
		session:  session,
	}
}

// Hash applies the pending operations to the reconstructed view and returns its
// new root. The view advances in place, so the operations are cleared rather
// than replayed on the next call.
//
// Hash cannot return an error, so if any error is encountered, it will be
// logged at error level and the zero hash is returned. [ffi.Reconstructed.Root]
// also returns the zero hash on an internal error, without its cause.
func (a *reconstructedAccountTrie) Hash() common.Hash {
	if len(a.updateOps) == 0 {
		return *a.root
	}
	if err := a.recon.Reconstruct(a.updateOps); err != nil {
		a.session.log.Error("hashing reconstructed account trie", zap.Error(err))
		return common.Hash{}
	}
	a.updateOps = nil
	*a.root = common.Hash(a.recon.Root())
	return *a.root
}

var errCommitReconstructedState = errors.New("cannot commit reconstructed state")

// Commit always returns [errCommitReconstructedState]. A reconstructed view
// exists only to serve historical reads and MUST NOT enter Firewood's proposal
// chain.
func (*reconstructedAccountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, errCommitReconstructedState
}

// Copy creates a copy of the [reconstructedAccountTrie] backed by a clone of the
// native view, carrying over the operations not yet applied to it. The clone is
// owned by the same session as the original.
func (a *reconstructedAccountTrie) Copy() *reconstructedAccountTrie {
	clone, err := a.session.clone(a.recon)
	if err != nil {
		a.session.log.Error("copying reconstructed account trie", zap.Error(err))
		return nil
	}

	root := *a.root
	tr := newReconstructedAccountTrie(clone, &root, a.session)
	tr.updateOps = slices.Clone(a.updateOps)
	return tr
}
