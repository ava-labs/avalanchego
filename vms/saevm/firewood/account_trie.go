// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
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
//  2. The [accountTrie.Hash] method actually creates the [ffi.Proposal], since Firewood cannot calculate
//     the hash of the trie without committing it.
//  3. the [accountTrie.GetAccount] and [accountTrie.GetStorage] methods cannot read from changes since
//     the most recent call to [accountTrie.Hash], and this is a very difficult problem to solve due
//     to account deletions on the `SELFDESTRUCT` opcode not manually calling [state.Trie.DeleteStorage].
//     Because of this, we have to rely on prefix deletions of the account to delete its associated storage.
//     Since the [state.StateDB] will never call a Get method on updated values, this is safe.
//
// Note this is not concurrent safe.
type accountTrie struct {
	*baseTrie
	parentRoot   common.Hash
	root         common.Hash
	pending      *proposalRef
	tdb          *TrieDB
	lastHashSize int
}

func newAccountTrie(root common.Hash, db *TrieDB) (*accountTrie, error) {
	reader, err := db.Firewood.Revision(ffi.Hash(root))
	if err != nil {
		return nil, err
	}
	return &accountTrie{
		baseTrie:   &baseTrie{reader: reader},
		parentRoot: root,
		root:       root,
		tdb:        db,
	}, nil
}

// Hash returns the current hash of the state trie.
// This will create the necessary proposals to guarantee that the changes can
// later be committed. Any new proposal will be tracked by the accountTrie
// until a call to [accountTrie.Commit].
//
// Any proposals created by this method will be freed once the accountTrie
// is garbage collected.
//
// Hash cannot return an error, so if any error is encountered, it will be
// logged at error level and the zero hash is returned.
func (a *accountTrie) Hash() common.Hash {
	if err := a.updateProposal(); err != nil {
		a.tdb.log.Error("hashing account trie", zap.Error(err))
		return common.Hash{}
	}
	return a.root
}

// updateProposal checks whether the pending proposal is out of date, and if it
// is, creates a new proposal with the addiotional changes and updates the
// internal state.
func (a *accountTrie) updateProposal() error {
	if a.lastHashSize == len(a.updateOps) {
		return nil
	}

	proposal, err := a.tdb.newProposal(a.parentRoot, a.updateOps)
	// TODO(#5506): Create [ffi.Reconstructed] to allow stateful RPCs.
	if err != nil {
		return err
	}

	a.pending = proposal
	a.reader = proposal.p
	a.root = proposal.root
	a.lastHashSize = len(a.updateOps) // Avoid re-hashing until next update
	return nil
}

// Commit returns the new root hash of the trie and a nil [trienode.NodeSet].
// The boolean input is ignored, as it is a relic of the StateTrie implementation.
// If the changes are not yet already tracked by the [TrieDB], they are created.
//
// The nil nodeset is not merged in [state.StateDB.Commit] and the merged
// nodeset is ignored by [TrieDB.Update], since all changes are tracked by the
// [ffi.Proposal].
func (a *accountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	// Creates proposal as side effect.
	if err := a.updateProposal(); err != nil {
		return common.Hash{}, nil, err
	}

	// The [state.StateDB] only calls [triedb.Database.Update] when the root
	// differs from the parent. Also, a.pending is only non-nil when the root
	// has changed.
	if a.root != a.parentRoot {
		a.tdb.trieCommit(a.pending)
	}
	return a.root, nil, nil
}

// Prove writes the inclusion or exclusion proof for the already hashed key to
// the provided writer.
//
// TODO(alarso16): Implement.
func (*accountTrie) Prove([]byte, ethdb.KeyValueWriter) error {
	return errProveNotImplemented
}

// Copy creates a copy of the [accountTrie].
func (a *accountTrie) Copy() *accountTrie {
	// This revision MUST be copied because it could refer to a proposal held
	// by this account trie. If the proposal is committed, the reader will no
	// longer be valid. However, an [ffi.Revision] will still be valid.
	reader, err := a.tdb.Firewood.Revision(ffi.Hash(a.parentRoot))
	if err != nil {
		a.tdb.log.Error("creating trie copy", zap.Error(err))
		return nil
	}
	return &accountTrie{
		baseTrie:   a.baseTrie.copy(reader),
		parentRoot: a.parentRoot,
		root:       a.root,
		tdb:        a.tdb,
	}
}
