// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ OptionBlock = &AbortBlock{}
	_ OptionBlock = &CommitBlock{}
)

type OptionBlock CommonBlockIntf

type AbortBlock struct {
	CommonBlock `serialize:"true"`
}

func NewAbortBlock(version uint16, parentID ids.ID, height uint64) (OptionBlock, error) {
	res := &AbortBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := CommonBlockIntf(res)
	bytes, err := Codec.Marshal(PreForkVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.Initialize(version, bytes)
}

type CommitBlock struct {
	CommonBlock `serialize:"true"`
}

func NewCommitBlock(version uint16, parentID ids.ID, height uint64) (OptionBlock, error) {
	res := &CommitBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := CommonBlockIntf(res)
	bytes, err := Codec.Marshal(PreForkVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.Initialize(version, bytes)
}
