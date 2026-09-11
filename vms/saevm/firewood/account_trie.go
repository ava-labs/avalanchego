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
)

var _ state.Trie = (*accountTrie)(nil)

// accountTrie should be used as a [state.Trie] for managing account states.
// Although it fulfills the [state.Trie] interface, it has some important differences:
//  1. [accountTrie.Commit] is not used as expected in the state package. The [storageTrie] doesn't return
//     values, and we thus rely on the shared [baseTrie]. Additionally, no [trienode.NodeSet] is
//     actually constructed, since Firewood manages nodes internally and the list of changes
//     is not needed externally.
//  2. The [accountTrie.Hash] method actually applies the changes to Firewood, since Firewood cannot
//     calculate the hash of the trie otherwise. If the parent root can be proposed on (it is the
//     Firewood tip or a not-yet-committed proposal), an [ffi.Proposal] is created so that the changes
//     can later be committed. Otherwise, an [ffi.Reconstructed] view over the historical revision is
//     built, which can be read and hashed but never committed. See [hasher].
//  3. the [accountTrie.GetAccount] and [accountTrie.GetStorage] methods cannot read from changes since
//     the most recent call to [accountTrie.Hash], and this is a very difficult problem to solve due
//     to account deletions on the `SELFDESTRUCT` opcode not manually calling [state.Trie.DeleteStorage].
//     Because of this, we have to rely on prefix deletions of the account to delete its associated storage.
//     Since the [state.StateDB] will never call a Get method on updated values, this is safe.
//
// Note this is not concurrent safe.
type accountTrie struct {
	*baseTrie
	revision *ffi.Revision
	hasher   hasher
	tdb      *TrieDB
}

func newAccountTrie(root common.Hash, db *TrieDB, currentOps []ffi.BatchOp) (*accountTrie, error) {
	revision, err := db.newRevision(root)
	if err != nil {
		return nil, err
	}
	hasher := newProposalHasher(db, root, revision)
	return newAccountTrieWithHasher(revision, hasher, db, currentOps), nil
}

func newAccountTrieWithHasher(revision *ffi.Revision, h hasher, db *TrieDB, currentOps []ffi.BatchOp) *accountTrie {
	return &accountTrie{
		baseTrie: &baseTrie{reader: h, updateOps: currentOps},
		revision: revision,
		hasher:   h,
		tdb:      db,
	}
}

// Hash returns the current hash of the state trie. This will apply any changes
// since the last call, which permits future calls to [accountTrie.Commit] if
// the changes are proposed on top of the tip of state.
//
// Any Firewood handles created by this method will be freed once the
// accountTrie is garbage collected.
//
// Hash cannot return an error, so if any error is encountered, it will be
// logged at error level and the zero hash is returned.
func (a *accountTrie) Hash() common.Hash {
	root, err := a.hash()
	if err != nil {
		a.tdb.log.Error("hashing account trie", zap.Error(err))
		return common.Hash{}
	}
	return root
}

// hash applies all pending updates via the [hasher] and returns the root. If the
// previous root was a historical revision, an [ffi.Reconstructed] will be made.
func (a *accountTrie) hash() (common.Hash, error) {
	root, err := a.hasher.hash(a.updateOps)
	if !errors.Is(err, errNotProposable) {
		return root, err
	}

	recon, err := a.tdb.newReconstructed(common.Hash(a.revision.Root()))
	if err != nil {
		return common.Hash{}, err
	}
	a.hasher = newReconstructedHasher(recon)
	a.reader = a.hasher
	return a.hasher.hash(a.updateOps)
}

// Commit returns the new root hash of the trie and a nil [trienode.NodeSet].
// The boolean input is ignored, as it is a relic of the StateTrie implementation.
// If the changes are not yet already tracked by the [TrieDB], they are created.
//
// The nil nodeset is not merged in [state.StateDB.Commit] and the merged
// nodeset is ignored by [TrieDB.Update], since all changes are tracked by the
// [ffi.Proposal]. The boolean input was intended to indicate whether to add
// the values as a leaf in the nodeset (corresponding to whether the caller
// expects this to be an account trie or not).
//
// Commit returns an error if the parent root cannot be proposed on, since such
// state can never be committed.
func (a *accountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	root, err := a.hash()
	if err != nil {
		return common.Hash{}, nil, err
	}

	if err := a.hasher.commit(); err != nil {
		return common.Hash{}, nil, fmt.Errorf("committing account trie: %w", err)
	}

	return root, nil, nil
}

// Copy creates a copy of the [accountTrie].
func (a *accountTrie) Copy() *accountTrie {
	h, err := a.hasher.copy()
	if err != nil {
		a.tdb.log.Error("copying account trie", zap.Error(err))
		return nil
	}
	return newAccountTrieWithHasher(a.revision, h, a.tdb, slices.Clone(a.updateOps))
}
