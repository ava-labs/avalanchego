// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &ApricotStandardBlock{}
	_ Block = &BlueberryStandardBlock{}
)

// TODO can we assume txs are initialized?
func NewStandardBlock(
	version uint16,
	timestamp uint64,
	parentID ids.ID,
	height uint64,
	txes []*txs.Tx,
) (Block, error) {
	switch version {
	case ApricotVersion:
		res := &ApricotStandardBlock{
			CommonBlock: CommonBlock{
				PrntID:       parentID,
				Hght:         height,
				BlkTimestamp: timestamp,
			},
			Txs: txes,
		}
		// We serialize this block as a Block so that it can be deserialized into a
		// Block
		blk := Block(res)
		bytes, err := Codec.Marshal(ApricotVersion, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}

		return res, res.Initialize(ApricotVersion, bytes)

	case BlueberryVersion:
		// Make sure we have the byte representation of
		// the [txes] so we can use them in the block.
		for _, tx := range txes {
			if err := tx.Sign(txs.Codec, nil); err != nil {
				return nil, fmt.Errorf("failed to sign block: %w", err)
			}
		}
		txsBytes := make([][]byte, len(txes))
		for i, tx := range txes {
			txBytes := tx.Bytes()
			txsBytes[i] = txBytes
		}
		res := &BlueberryStandardBlock{
			CommonBlock: CommonBlock{
				PrntID:       parentID,
				Hght:         height,
				BlkTimestamp: timestamp,
			},
			TxsBytes: txsBytes,
			Txs:      txes,
		}
		// We serialize this block as a Block so that it can be deserialized into a
		// Block
		blk := Block(res)
		bytes, err := Codec.Marshal(BlueberryVersion, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}
		return res, res.Initialize(BlueberryVersion, bytes)

	default:
		return nil, fmt.Errorf("unsopported block version %d", version)
	}
}

type ApricotStandardBlock struct {
	CommonBlock `serialize:"true"`

	Txs []*txs.Tx `serialize:"true" json:"txs"`
}

func (asb *ApricotStandardBlock) Initialize(version uint16, bytes []byte) error {
	if err := asb.CommonBlock.Initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range asb.Txs {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (asb *ApricotStandardBlock) BlockTxs() []*txs.Tx { return asb.Txs }

func (asb *ApricotStandardBlock) Visit(v Visitor) error {
	return v.ApricotStandardBlock(asb)
}

type BlueberryStandardBlock struct {
	CommonBlock `serialize:"true"`

	TxsBytes [][]byte `serialize:"false" blueberry:"true" json:"txs"`

	Txs []*txs.Tx
}

func (bsb *BlueberryStandardBlock) Initialize(version uint16, bytes []byte) error {
	if err := bsb.CommonBlock.Initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	bsb.Txs = make([]*txs.Tx, len(bsb.TxsBytes))
	for i, txBytes := range bsb.TxsBytes {
		var tx txs.Tx
		if _, err := txs.Codec.Unmarshal(txBytes, &tx); err != nil {
			return fmt.Errorf("failed unmarshalling tx in blueberry block: %w", err)
		}
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
		bsb.Txs[i] = &tx
	}
	return nil
}

func (bsb *BlueberryStandardBlock) BlockTxs() []*txs.Tx { return bsb.Txs }

func (bsb *BlueberryStandardBlock) Visit(v Visitor) error {
	return v.BlueberryStandardBlock(bsb)
}
