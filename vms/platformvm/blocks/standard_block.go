// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &StandardBlock{}

type StandardBlock struct {
	CommonBlock `serialize:"true"`

	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (sb *StandardBlock) initialize(bytes []byte) error {
	sb.CommonBlock.initialize(bytes)
	for _, tx := range sb.Transactions {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to initialize tx: %w", err)
		}
	}
	return nil
}

func (sb *StandardBlock) Txs() []*txs.Tx { return sb.Transactions }

func (sb *StandardBlock) Visit(v Visitor) error {
	return v.StandardBlock(sb)
}

func NewStandardBlock(
	parentID ids.ID,
	height uint64,
	txes []*txs.Tx,
) (*StandardBlock, error) {
	blk := &StandardBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Transactions: txes,
	}
	return blk, initialize(blk)
}
