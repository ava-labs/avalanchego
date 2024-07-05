// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Hasher = (*rlpHasher)(nil)

type rlpHasher struct{}

func (*rlpHasher) HashOrEmbedNode(n, parent *node) ([]byte, ids.ID) {
	var (
		id    ids.ID
		hash  []byte
		embed []byte
	)
	embedOrHash := n.encodeRLPWithShortNode(parent)
	hash = embedOrHash
	if len(embedOrHash) < 32 {
		embed = embedOrHash
		hash = hashData(embedOrHash)
	}

	copy(id[:], hash)
	return embed, id
}

func (r *rlpHasher) HashNode(n, parent *node) ids.ID {
	_, id := r.HashOrEmbedNode(n, parent)
	return id
}

func (*rlpHasher) HashValue(value []byte) ids.ID {
	var id ids.ID
	hash := hashData(value)
	copy(id[:], hash)
	return id
}

func (*rlpHasher) EmptyRoot() ids.ID {
	return ids.ID(hashData(rlp.EmptyString))
}
