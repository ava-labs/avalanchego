// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ Block = &StandardBlock{}

type StandardBlock struct {
	CommonBlock `serialize:"true"`

	Txs []*signed.Tx `serialize:"true" json:"txs"`
}

func (sb *StandardBlock) Initialize(bytes []byte) error {
	if err := sb.CommonBlock.Initialize(bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(unsigned.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (sb *StandardBlock) BlockTxs() []*signed.Tx { return sb.Txs }

func NewStandardBlock(parentID ids.ID, height uint64, txs []*signed.Tx) (*StandardBlock, error) {
	res := &StandardBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Txs: txs,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	for _, tx := range res.Txs {
		if err := tx.Sign(unsigned.Codec, nil); err != nil {
			return nil, fmt.Errorf("failed to sign block: %w", err)
		}
	}

	return res, res.Initialize(bytes)
}
