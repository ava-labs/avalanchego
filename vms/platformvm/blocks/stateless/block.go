// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

type Statuser interface {
	Status(blkID ids.ID) choices.Status
}

type Timestamper interface {
	Timestamp(ids.ID) time.Time
}

// Interface introduced for marshalling/unmarshalling
type Block interface {
	Initialize(bytes []byte) error
	ID() ids.ID
	Bytes() []byte
	Parent() ids.ID
	Height() uint64

	// BlockTxs returns list of transactions
	// contained in the block
	BlockTxs() []*txs.Tx

	Visit(visitor Visitor) error
}
