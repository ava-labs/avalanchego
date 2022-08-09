// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &ApricotAtomicBlock{}

// ApricotAtomicBlock being accepted results in the atomic transaction contained in the
// block to be accepted and committed to the chain.
type ApricotAtomicBlock struct {
	ApricotCommonBlock `serialize:"true"`

	Tx *txs.Tx `serialize:"true" json:"tx"`
}

// NewApricotAtomicBlock assumes [tx] is initialized
func NewApricotAtomicBlock(
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*ApricotAtomicBlock, error) {
	res := &ApricotAtomicBlock{
		ApricotCommonBlock: ApricotCommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}

	return res, initialize(Block(res))
}

func (b *ApricotAtomicBlock) initialize(bytes []byte) error {
	if err := b.ApricotCommonBlock.initialize(bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	return b.Tx.Sign(txs.Codec, nil)
}

func (b *ApricotAtomicBlock) Txs() []*txs.Tx { return []*txs.Tx{b.Tx} }

func (b *ApricotAtomicBlock) Visit(v Visitor) error {
	return v.ApricotAtomicBlock(b)
}
