// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

const HashLength = 32

type nodeChildren map[byte]child

type child struct {
	compressedKey Key
	id            ids.ID
	hasValue      bool
}

// Returns and caches the ID of this node.
func calculateID(key Key, metrics merkleMetrics, n nodeChildren, value maybe.Maybe[[]byte]) ids.ID {
	metrics.HashCalculated()
	return hashing.ComputeHash256Array(codec.encodeHashValues(key, n, value))
}

func getValueDigest(val maybe.Maybe[[]byte]) maybe.Maybe[[]byte] {
	if val.IsNothing() || len(val.Value()) <= HashLength {
		return val
	}
	return maybe.Some(hashing.ComputeHash256(val.Value()))
}

// Returns the ProofNode representation of this node.
func asProofNode(key Key, n nodeChildren, value maybe.Maybe[[]byte]) ProofNode {
	pn := ProofNode{
		Key:         key,
		Children:    make(map[byte]ids.ID, len(n)),
		ValueOrHash: getValueDigest(value),
	}
	for index, entry := range n {
		pn.Children[index] = entry.id
	}
	return pn
}
