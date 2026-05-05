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

// reconstructedStateAccessor wraps a [state.Database] and overrides OpenTrie
// and OpenStorageTrie to return reconstructed tries backed by an [ffi.Reconstructed].
type reconstructedStateAccessor struct {
	state.Database

	recon *ffi.Reconstructed
}

// NewReconstructedStateAccessor creates a [state.Database] that opens tries
// backed by the given [ffi.Reconstructed] view. The [ffi.Reconstructed] view is
// mutated in place by trie operations, so the caller must not use it concurrently.
//
// The provided db must have been returned by [NewStateAccessor].
func NewReconstructedStateAccessor(db state.Database, recon *ffi.Reconstructed) (state.Database, error) {
	if _, ok := db.(*stateAccessor); !ok {
		return nil, fmt.Errorf("expected *stateAccessor, got %T", db)
	}
	return &reconstructedStateAccessor{
		Database: db,
		recon:    recon,
	}, nil
}

// OpenTrie opens an account trie backed by the [ffi.Reconstructed] view.
// Only the view's current root is accepted; passing an arbitrary root will
// return an error.
func (s *reconstructedStateAccessor) OpenTrie(hash common.Hash) (state.Trie, error) {
	currRoot := common.Hash(s.recon.Root())
	if currRoot != hash {
		return nil, fmt.Errorf("expected root hash %s but got %s", hash, currRoot)
	}

	t, err := newReconstructedAccountTrie(s.recon)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// OpenStorageTrie opens a reconstructed storage trie wrapping the account trie.
//
//nolint:revive // removing names loses context.
func (*reconstructedStateAccessor) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, self state.Trie) (state.Trie, error) {
	accountTrie, ok := self.(*reconstructedAccountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type for reconstructed storage: %T", self)
	}
	return newStorageTrie(&accountTrie.baseTrie), nil
}

// CopyTrie returns a deep copy of the given trie.
func (*reconstructedStateAccessor) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *reconstructedAccountTrie:
		// reconstructedAccountTrie is not concurrent-safe
		return nil
	case *storageTrie:
		return nil
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}
