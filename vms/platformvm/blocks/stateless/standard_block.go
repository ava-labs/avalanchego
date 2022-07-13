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

func NewStandardBlock(
	version uint16,
	timestamp uint64,
	parentID ids.ID,
	height uint64,
	txes []*txs.Tx,
) (Block, error) {
	// make sure txs to be included in the block
	// are duly initialized
	for _, tx := range txes {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return nil, fmt.Errorf("failed to sign block: %w", err)
		}
	}

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
		bytes, err := Codec.Marshal(version, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}

		return res, res.Initialize(version, bytes)

	case BlueberryVersion:
		txsBytes := make([][]byte, 0, len(txes))
		for _, tx := range txes {
			txsBytes = append(txsBytes, tx.Bytes())
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
		bytes, err := Codec.Marshal(version, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}
		return res, res.Initialize(version, bytes)

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
	return v.VisitApricotStandardBlock(asb)
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

	txes := make([]*txs.Tx, 0, len(bsb.TxsBytes))
	for _, txBytes := range bsb.TxsBytes {
		var tx txs.Tx
		_, err := txs.Codec.Unmarshal(txBytes, &tx)
		if err != nil {
			return fmt.Errorf("failed unmarshalling tx in blueberry block: %w", err)
		}
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
		txes = append(txes, &tx)
	}
	bsb.Txs = txes
	return nil
}

func (bsb *BlueberryStandardBlock) BlockTxs() []*txs.Tx { return bsb.Txs }

func (bsb *BlueberryStandardBlock) Visit(v Visitor) error {
	return v.VisitBlueberryStandardBlock(bsb)
}
