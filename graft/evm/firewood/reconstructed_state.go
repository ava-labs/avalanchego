// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

var _ state.Database = (*reconstructedStateAccessor)(nil)

// reconstructedStateAccessor wraps a state.Database and overrides OpenTrie
// and OpenStorageTrie to return reconstructed tries backed by an ffi.Reconstructed.
type reconstructedStateAccessor struct {
	state.Database
	reconstructed *ffi.Reconstructed
	currentTrie   *reconstructedAccountTrie // tracks the last trie opened via OpenTrie
}

// NewReconstructedStateAccessor creates a state.Database that opens tries
// backed by the given Reconstructed view. The caller retains ownership of
// the Reconstructed handle.
func NewReconstructedStateAccessor(db state.Database, recon *ffi.Reconstructed) state.Database {
	return &reconstructedStateAccessor{
		Database:      db,
		reconstructed: recon,
	}
}

// OpenTrie opens a reconstructedAccountTrie backed by the Reconstructed view.
// The root parameter is ignored -- the trie always reads from the current
// Reconstructed state.
func (s *reconstructedStateAccessor) OpenTrie(_ common.Hash) (state.Trie, error) {
	t, err := newReconstructedAccountTrie(s.reconstructed)
	if err != nil {
		return nil, err
	}
	s.currentTrie = t
	return t, nil
}

// OpenStorageTrie opens a reconstructed storage trie wrapping the account trie.
//
//nolint:revive // removing names loses context.
func (*reconstructedStateAccessor) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, self state.Trie) (state.Trie, error) {
	acctTrie, ok := self.(*reconstructedAccountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type for reconstructed storage: %T", self)
	}
	return newStorageTrie(&acctTrie.baseAccountTrie), nil
}

// UpdateOps returns the accumulated batch operations from the current account trie.
// This should be called after Finalise() to extract the operations before
// chaining Reconstruct().
func (s *reconstructedStateAccessor) UpdateOps() []ffi.BatchOp {
	if s.currentTrie == nil {
		return nil
	}
	return s.currentTrie.UpdateOps()
}

// ExtractUpdateOps extracts the accumulated BatchOps from a state.Database
// if it is backed by a reconstructedStateAccessor. Returns nil if the database
// is not a reconstructedStateAccessor or has no current trie.
func ExtractUpdateOps(db state.Database) []ffi.BatchOp {
	if accessor, ok := db.(*reconstructedStateAccessor); ok {
		return accessor.UpdateOps()
	}
	return nil
}

// CopyTrie returns a deep copy of the given trie.
func (*reconstructedStateAccessor) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *reconstructedAccountTrie:
		return t.Copy()
	case *storageTrie:
		return nil
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}
