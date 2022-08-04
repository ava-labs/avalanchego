// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &AtomicBlock{}

// AtomicBlock being accepted results in the atomic transaction contained in the
// block to be accepted and committed to the chain.
type AtomicBlock struct {
	ApricotCommonBlock `serialize:"true"`

	Tx *txs.Tx `serialize:"true" json:"tx"`
}

// NewAtomicBlock assumes [tx] is initialized
func NewAtomicBlock(
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*AtomicBlock, error) {
	res := &AtomicBlock{
		ApricotCommonBlock: ApricotCommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(version.ApricotBlockVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.initialize(version.ApricotBlockVersion, bytes)
}

func (ab *AtomicBlock) initialize(version uint16, bytes []byte) error {
	if err := ab.ApricotCommonBlock.initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	if err := ab.Tx.Sign(txs.Codec, nil); err != nil {
		return fmt.Errorf("failed to initialize tx: %w", err)
	}
	return nil
}

func (ab *AtomicBlock) Txs() []*txs.Tx { return []*txs.Tx{ab.Tx} }

func (ab *AtomicBlock) Visit(v Visitor) error {
	return v.AtomicBlock(ab)
}
