// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &AtomicBlock{}

// AtomicBlock being accepted results in the atomic transaction contained in the
// block to be accepted and committed to the chain.
type AtomicBlock struct {
	CommonBlock `serialize:"true"`
	Tx          *txs.Tx `serialize:"true" json:"tx"`
}

func (ab *AtomicBlock) initialize(bytes []byte) error {
	ab.CommonBlock.initialize(bytes)
	if err := ab.Tx.Sign(txs.Codec, nil); err != nil {
		return fmt.Errorf("failed to initialize tx: %w", err)
	}
	return nil
}

func (ab *AtomicBlock) Txs() []*txs.Tx { return []*txs.Tx{ab.Tx} }

func (ab *AtomicBlock) Visit(v Visitor) error {
	return v.AtomicBlock(ab)
}

func NewAtomicBlock(
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*AtomicBlock, error) {
	blk := &AtomicBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}
	return blk, initialize(blk)
}
