// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &StandardBlock{}

type StandardBlock struct {
	CommonBlock `serialize:"true"`

	Txs []*txs.Tx `serialize:"true" json:"txs"`
}

func (sb *StandardBlock) initialize(bytes []byte) error {
	if err := sb.CommonBlock.initialize(bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (sb *StandardBlock) BlockTxs() []*txs.Tx { return sb.Txs }

func (sb *StandardBlock) Visit(v Visitor) error {
	return v.StandardBlock(sb)
}

func NewStandardBlock(
	parentID ids.ID,
	height uint64,
	txes []*txs.Tx,
) (*StandardBlock, error) {
	res := &StandardBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Txs: txes,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	for _, tx := range res.Txs {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return nil, fmt.Errorf("failed to sign block: %w", err)
		}
	}

	return res, res.initialize(bytes)
}
