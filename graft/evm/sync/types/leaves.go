// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"context"

	"github.com/ava-labs/libevm/common"
)

// LeafRange is a contiguous run of trie leaves to read.
type LeafRange struct {
	Root common.Hash
	// Account owns a storage trie, and is zero for the account trie.
	Account common.Hash
	// Start and End bound the run inclusively. Nil means the trie's edge.
	Start []byte
	End   []byte
	Limit uint16
}

// Leaves is a verified run in ascending key order, Keys and Vals aligned.
type Leaves struct {
	Keys [][]byte
	Vals [][]byte
	// More reports whether leaves remain to the right of the last key.
	More bool
}

// LeafFetcher reads leaf ranges from the network. Implementations own range
// proof verification, so a caller never sees an unproven range.
type LeafFetcher interface {
	FetchLeaves(ctx context.Context, req LeafRange) (Leaves, error)
}
