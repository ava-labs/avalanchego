// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ Block = &ProposalBlock{}

// As is, this is duplication of atomic block. But let's tolerate some code duplication for now
type ProposalBlock struct {
	CommonBlock `serialize:"true"`

	Tx signed.Tx `serialize:"true" json:"tx"`
}

func (pb *ProposalBlock) Initialize(bytes []byte) error {
	if err := pb.CommonBlock.Initialize(bytes); err != nil {
		return err
	}

	unsignedBytes, err := unsigned.Codec.Marshal(unsigned.Version, &pb.Tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to marshal unsigned tx: %w", err)
	}
	signedBytes, err := unsigned.Codec.Marshal(unsigned.Version, &pb.Tx)
	if err != nil {
		return fmt.Errorf("failed to marshal tx: %w", err)
	}
	pb.Tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

func (pb *ProposalBlock) BlockTxs() []*signed.Tx { return []*signed.Tx{&pb.Tx} }

func NewProposalBlock(parentID ids.ID, height uint64, tx signed.Tx) (*ProposalBlock, error) {
	res := &ProposalBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	if err := tx.Sign(unsigned.Codec, nil); err != nil {
		return nil, fmt.Errorf("failed to sign block: %w", err)
	}

	return res, res.Initialize(bytes)
}
