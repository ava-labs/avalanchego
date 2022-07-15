// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Interface introduced for marshalling/unmarshalling
type Block interface {
	initialize(bytes []byte) error
	ID() ids.ID
	Bytes() []byte
	Parent() ids.ID
	Height() uint64

	// BlockTxs returns list of transactions
	// contained in the block
	BlockTxs() []*txs.Tx

	Visit(visitor Visitor) error
}
