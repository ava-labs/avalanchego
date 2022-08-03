// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Block defines the common stateless interface for all blocks
type Block interface {
	ID() ids.ID
	Parent() ids.ID
	Bytes() []byte
	Height() uint64

	// Txs returns list of transactions contained in the block
	Txs() []*txs.Tx

	Visit(visitor Visitor) error

	initialize(bytes []byte) error
}
