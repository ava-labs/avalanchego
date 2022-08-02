// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &ApricotProposalBlock{}
	_ Block = &BlueberryProposalBlock{}
)

// NewProposalBlock assumes [tx] is initialized
func NewProposalBlock(
	version uint16,
	timestamp uint64,
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (Block, error) {
	switch version {
	case ApricotVersion:
		res := &ApricotProposalBlock{
			CommonBlock: CommonBlock{
				PrntID:       parentID,
				Hght:         height,
				BlkTimestamp: timestamp,
			},
			Tx: tx,
		}

		// We serialize this block as a Block so that it can be deserialized into a
		// Block
		blk := Block(res)
		bytes, err := Codec.Marshal(ApricotVersion, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}
		return res, res.initialize(ApricotVersion, bytes)

	case BlueberryVersion:
		res := &BlueberryProposalBlock{
			CommonBlock: CommonBlock{
				PrntID:       parentID,
				Hght:         height,
				BlkTimestamp: timestamp,
			},
			TxBytes: tx.Bytes(),
			Tx:      tx,
		}

		// We serialize this block as a Block so that it can be deserialized into a
		// Block
		blk := Block(res)
		bytes, err := Codec.Marshal(BlueberryVersion, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}
		return res, res.initialize(BlueberryVersion, bytes)

	default:
		return nil, fmt.Errorf("unsupported block version %d", version)
	}
}

// As is, this is duplication of atomic block. But let's tolerate some code duplication for now
type ApricotProposalBlock struct {
	CommonBlock `serialize:"true"`

	Tx *txs.Tx `serialize:"true" json:"tx"`
}

func (b *ApricotProposalBlock) initialize(version uint16, bytes []byte) error {
	if err := b.CommonBlock.initialize(version, bytes); err != nil {
		return err
	}
	return b.Tx.Sign(txs.Codec, nil)
}

func (b *ApricotProposalBlock) BlockTxs() []*txs.Tx { return []*txs.Tx{b.Tx} }

func (b *ApricotProposalBlock) Visit(v Visitor) error {
	return v.ApricotProposalBlock(b)
}

type BlueberryProposalBlock struct {
	CommonBlock `serialize:"true"`

	TxBytes []byte `serialize:"false" blueberry:"true" json:"txs"`

	Tx *txs.Tx
}

func (b *BlueberryProposalBlock) initialize(version uint16, bytes []byte) error {
	if err := b.CommonBlock.initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	// [Tx] may be initialized from NewProposalBlock
	// TODO can we do this a better way?
	if b.Tx == nil {
		var tx txs.Tx
		if _, err := txs.Codec.Unmarshal(b.TxBytes, &tx); err != nil {
			return fmt.Errorf("failed unmarshalling tx in Blueberry block: %w", err)
		}
		b.Tx = &tx
		if err := b.Tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (b *BlueberryProposalBlock) BlockTxs() []*txs.Tx { return []*txs.Tx{b.Tx} }

func (b *BlueberryProposalBlock) Visit(v Visitor) error {
	return v.BlueberryProposalBlock(b)
}
