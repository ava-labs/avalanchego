// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &ProposalBlock{}

// As is, this is duplication of atomic block. But let's tolerate some code duplication for now
type ProposalBlock struct {
	CommonBlock `serialize:"true"`

	Tx *txs.Tx `serialize:"true" json:"tx"`
}

func (pb *ProposalBlock) initialize(bytes []byte) error {
	if err := pb.CommonBlock.initialize(bytes); err != nil {
		return err
	}
	return pb.Tx.Sign(txs.Codec, nil)
}

func (pb *ProposalBlock) Txs() []*txs.Tx { return []*txs.Tx{pb.Tx} }

func (pb *ProposalBlock) Visit(v Visitor) error {
	return v.ProposalBlock(pb)
}

func NewProposalBlock(
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*ProposalBlock, error) {
	blk := &ProposalBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}
	return blk, initialize(blk)
}
