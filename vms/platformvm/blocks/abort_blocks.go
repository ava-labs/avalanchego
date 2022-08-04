// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &ApricotAbortBlock{}
	_ Block = &BlueberryAbortBlock{}
)

type ApricotAbortBlock struct {
	ApricotCommonBlock `serialize:"true"`
}

func (*ApricotAbortBlock) Txs() []*txs.Tx { return nil }

func (b *ApricotAbortBlock) Visit(v Visitor) error {
	return v.ApricotAbortBlock(b)
}

type BlueberryAbortBlock struct {
	BlueberryCommonBlock `serialize:"true"`
}

func (*BlueberryAbortBlock) Txs() []*txs.Tx { return nil }

func (b *BlueberryAbortBlock) Visit(v Visitor) error {
	return v.BlueberryAbortBlock(b)
}

func NewAbortBlock(
	blkVersion uint16,
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (Block, error) {
	switch blkVersion {
	case version.ApricotBlockVersion:
		res := &ApricotAbortBlock{
			ApricotCommonBlock: ApricotCommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
		}

		// We serialize this block as a Block so that it can be deserialized into a
		// Block
		blk := Block(res)
		bytes, err := Codec.Marshal(Version, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}

		return res, res.initialize(blkVersion, bytes)

	case version.BlueberryBlockVersion:
		res := &BlueberryAbortBlock{
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
		bytes, err := Codec.Marshal(Version, &blk)
		if err != nil {
			return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
		}

		return res, res.initialize(blkVersion, bytes)

	default:
		return nil, fmt.Errorf("unsupported block version %d", blkVersion)
	}
}
