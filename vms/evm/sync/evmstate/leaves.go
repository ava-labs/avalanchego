// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import "github.com/ava-labs/libevm/common"

// LeafRange is a run of leaves to fetch from one trie.
type LeafRange struct {
	Root    common.Hash
	Account common.Hash // zero for the account trie
	Start   []byte      // nil for the trie's first key
	End     []byte      // inclusive, nil for the trie's last key
	Limit   uint16
}

// Leaves is a verified run of trie leaves in key order.
type Leaves struct {
	Keys [][]byte
	Vals [][]byte
}
