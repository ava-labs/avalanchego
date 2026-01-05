// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Block defines the common stateless interface for all blocks
type Block interface {
	snow.ContextInitializable
	ID() ids.ID
	Parent() ids.ID
	Bytes() []byte
	Height() uint64

	// Txs returns list of transactions contained in the block
	Txs() []*txs.Tx

	// Visit calls [visitor] with this block's concrete type
	Visit(visitor Visitor) error

	// note: initialize does not assume that block transactions
	// are initialized, and initializes them itself if they aren't.
	initialize(bytes []byte) error
}

type BanffBlock interface {
	Block
	Timestamp() time.Time
}

func initialize(blk Block, commonBlk *CommonBlock) error {
	// We serialize this block as a pointer so that it can be deserialized into
	// a Block
	bytes, err := Codec.Marshal(CodecVersion, &blk)
	if err != nil {
		return fmt.Errorf("couldn't marshal block: %w", err)
	}

	commonBlk.initialize(bytes)
	return nil
}
