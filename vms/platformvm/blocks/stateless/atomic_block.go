// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ Block = &AtomicBlock{}

// AtomicBlock being accepted results in the atomic transaction contained in the
// block to be accepted and committed to the chain.
type AtomicBlock struct {
	CommonBlock `serialize:"true"`

	Tx signed.Tx `serialize:"true" json:"tx"`
}

func (ab *AtomicBlock) Initialize(bytes []byte) error {
	if err := ab.CommonBlock.Initialize(bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	unsignedBytes, err := unsigned.Codec.Marshal(unsigned.Version, &ab.Tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to marshal unsigned tx: %w", err)
	}
	signedBytes, err := unsigned.Codec.Marshal(unsigned.Version, &ab.Tx)
	if err != nil {
		return fmt.Errorf("failed to marshal tx: %w", err)
	}
	ab.Tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

func (ab *AtomicBlock) BlockTxs() []*signed.Tx { return []*signed.Tx{&ab.Tx} }

func NewAtomicBlock(parentID ids.ID, height uint64, tx signed.Tx) (*AtomicBlock, error) {
	res := &AtomicBlock{
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
