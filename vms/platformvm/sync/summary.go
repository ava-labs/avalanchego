// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var _ block.StateSummary = (*Summary)(nil)

// Summary provides the information necessary to sync a node starting
// at the given block.
type Summary struct {
	BlockHeight uint64 `serialize:"true"`
	BlockID     ids.ID `serialize:"true"`
	BlockRootID ids.ID `serialize:"true"`

	// Invariant: non-nil.
	summaryID ids.ID
	// Invariant: non-nil.
	bytes []byte
	// Invariant: non-nil.
	acceptFunc func(Summary) (block.StateSyncMode, error)
}

func NewSummaryFromBytes(
	summaryBytes []byte,
	acceptFunc func(Summary) (block.StateSyncMode, error),
) (Summary, error) {
	var summary Summary
	if codecVersion, err := Codec.Unmarshal(summaryBytes, &summary); err != nil {
		return Summary{}, err
	} else if codecVersion != Version {
		return Summary{}, fmt.Errorf("failed to parse syncable summary due to unexpected codec version (%d != %d)", codecVersion, Version)
	}

	summary.bytes = summaryBytes
	summaryID, err := ids.ToID(hashing.ComputeHash256(summaryBytes))
	if err != nil {
		return Summary{}, err
	}
	summary.summaryID = summaryID
	summary.acceptFunc = acceptFunc
	return summary, nil
}

func NewSummary(
	blockID ids.ID,
	blockNumber uint64,
	blockRootID ids.ID,
	acceptFunc func(Summary) (block.StateSyncMode, error),
) (Summary, error) {
	summary := Summary{
		BlockHeight: blockNumber,
		BlockID:     blockID,
		BlockRootID: blockRootID,
		acceptFunc:  acceptFunc,
	}
	bytes, err := Codec.Marshal(Version, &summary)
	if err != nil {
		return Summary{}, err
	}

	summary.bytes = bytes
	summaryID, err := ids.ToID(hashing.ComputeHash256(bytes))
	if err != nil {
		return Summary{}, err
	}
	summary.summaryID = summaryID

	return summary, nil
}

func (s Summary) Bytes() []byte {
	return s.bytes
}

func (s Summary) Height() uint64 {
	return s.BlockHeight
}

func (s Summary) ID() ids.ID {
	return s.summaryID
}

func (s Summary) String() string {
	return fmt.Sprintf(
		"Summary(BlockID=%s, BlockNumber=%d, BlockRootID=%s)",
		s.BlockID, s.BlockHeight, s.BlockRootID,
	)
}

func (s Summary) Accept(context.Context) (block.StateSyncMode, error) {
	return s.acceptFunc(s)
}
