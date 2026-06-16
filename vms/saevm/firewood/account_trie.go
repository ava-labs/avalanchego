// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/trie/trienode"
	"go.uber.org/zap"
)

var _ state.Trie = (*accountTrie)(nil)

// accountTrie implements [state.Trie] for managing account states.
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
	parentRoot common.Hash
	pending    *proposalRef
	fw         *TrieDB
}

func newAccountTrie(root common.Hash, db *TrieDB) (*accountTrie, error) {
	reader, err := db.Firewood.Revision(ffi.Hash(root))
	if err != nil {
		return nil, err
	}
	return &accountTrie{
		// cleared and storageOpsAfterClear are left nil; set.Set treats a nil
		// set as empty and self-initializes on first Add.
		baseTrie:   &baseTrie{reader: reader, root: root},
		parentRoot: root,
		fw:         db,
	}, nil
}

// Hash returns the current hash of the state trie.
// This will create the necessary proposals to guarantee that the changes can
// later be committed. Any new proposal will be tracked by the accountTrie
// until a call to [accountTrie.Commit].
//
// Any proposals created by this method will be freed once the accountTrie
// is garbage collected.
func (a *accountTrie) Hash() common.Hash {
	hash, err := a.hash()
	if err != nil {
		a.fw.log.Error("hashing account trie", zap.Error(err))
		return common.Hash{}
	}
	return hash
}

func (a *accountTrie) hash() (common.Hash, error) {
	if !a.hasChanges {
		return a.root, nil
	}

	proposal, err := a.fw.trieHash(a.parentRoot, a.updateOps)
	switch {
	case err != nil:
		return common.Hash{}, err
	case proposal == nil:
		// TODO(#5506): Create [ffi.Reconstructed] to allow stateful RPCs.
		return common.Hash{}, fmt.Errorf("base revision %#x is not proposable", a.parentRoot)
	}

	// Best effort drop of previous reader (and thus any associated proposal).
	// Use new proposal for all future reads.
	if err := a.reader.Drop(); err != nil {
		a.fw.log.Warn("dropping previous trie reader", zap.Error(err))
	}

	a.pending = proposal
	a.reader = proposal.p
	a.root = proposal.root
	a.hasChanges = false // Avoid re-hashing until next update
	return a.root, nil
}

// Commit returns the new root hash of the trie and an empty [trienode.NodeSet].
// The boolean input is ignored, as it is a relic of the StateTrie implementation.
// If the changes are not yet already tracked by the [TrieDB], they are created.
func (a *accountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	// Creates proposal as side effect.
	hash, err := a.hash()
	if err != nil {
		return common.Hash{}, nil, err
	}

	// The [state.StateDB] will only call [triedb.Database.Update] if the returned root differs from the parent.
	if hash == a.parentRoot {
		return a.parentRoot, nil, nil
	}

	a.fw.trieCommit(a.pending)
	return hash, trienode.NewNodeSet(common.Hash{}), nil
}

// Copy creates a copy of the [accountTrie].
func (a *accountTrie) Copy() *accountTrie {
	reader, err := a.fw.Firewood.Revision(ffi.Hash(a.parentRoot))
	if err != nil {
		a.fw.log.Error("creating trie copy", zap.Error(err))
		return nil
	}
	return &accountTrie{
		baseTrie:   a.baseTrie.copy(reader),
		parentRoot: a.parentRoot,
		fw:         a.fw,
	}
}
