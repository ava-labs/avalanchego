// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &ApricotCommitBlock{}
	_ Block = &BlueberryCommitBlock{}
)

type ApricotCommitBlock struct {
	ApricotCommonBlock `serialize:"true"`
}

func (*ApricotCommitBlock) Txs() []*txs.Tx { return nil }

func (b *ApricotCommitBlock) Visit(v Visitor) error {
	return v.ApricotCommitBlock(b)
}

type BlueberryCommitBlock struct {
	BlueberryCommonBlock `serialize:"true"`
}

func (*BlueberryCommitBlock) Txs() []*txs.Tx { return nil }

func (b *BlueberryCommitBlock) Visit(v Visitor) error {
	return v.BlueberryCommitBlock(b)
}

func NewCommitBlock(
	blkVersion uint16,
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (Block, error) {
	switch blkVersion {
	case ApricotVersion:
		res := &ApricotCommitBlock{
			ApricotCommonBlock: ApricotCommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
		}

		// We serialize this block as a Block so that it can be deserialized into a
		// Block
		blk := Block(res)
		bytes, err := Codec.Marshal(blkVersion, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}

		return res, res.initialize(blkVersion, bytes)

	case BlueberryVersion:
		res := &BlueberryCommitBlock{
			BlueberryCommonBlock: BlueberryCommonBlock{
				ApricotCommonBlock: ApricotCommonBlock{
					PrntID: parentID,
					Hght:   height,
				},
				BlkTimestamp: uint64(timestamp.Unix()),
			},
		}

		// We serialize this block as a Block so that it can be deserialized into a
		// Block
		blk := Block(res)
		bytes, err := Codec.Marshal(blkVersion, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}

		return res, res.initialize(blkVersion, bytes)

	default:
		return nil, fmt.Errorf("unsupported block version %d", blkVersion)
	}
}
