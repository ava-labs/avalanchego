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

var _ block.StateSummary = (*SyncSummary)(nil)

// SyncSummary provides the information necessary to sync a node starting
// at the given block.
type SyncSummary struct {
	BlockNumber uint64 `serialize:"true"`
	BlockID     ids.ID `serialize:"true"`
	BlockRoot   ids.ID `serialize:"true"`

	// Invariant: non-nil.
	summaryID ids.ID
	// Invariant: non-nil.
	bytes []byte
	// Invariant: non-nil.
	acceptFunc func(SyncSummary) (block.StateSyncMode, error)
}

func NewSyncSummaryFromBytes(summaryBytes []byte, acceptImpl func(SyncSummary) (block.StateSyncMode, error)) (SyncSummary, error) {
	var summary SyncSummary
	if codecVersion, err := Codec.Unmarshal(summaryBytes, &summary); err != nil {
		return SyncSummary{}, err
	} else if codecVersion != Version {
		return SyncSummary{}, fmt.Errorf("failed to parse syncable summary due to unexpected codec version (%d != %d)", codecVersion, Version)
	}

	summary.bytes = summaryBytes
	summaryID, err := ids.ToID(summaryBytes)
	if err != nil {
		return SyncSummary{}, err
	}
	summary.summaryID = summaryID
	summary.acceptFunc = acceptImpl
	return summary, nil
}

func NewSyncSummary(blockID ids.ID, blockNumber uint64, blockRoot ids.ID) (SyncSummary, error) {
	summary := SyncSummary{
		BlockNumber: blockNumber,
		BlockID:     blockID,
		BlockRoot:   blockRoot,
	}
	bytes, err := Codec.Marshal(Version, &summary)
	if err != nil {
		return SyncSummary{}, err
	}

	summary.bytes = bytes
	summaryID, err := ids.ToID(hashing.ComputeHash256(bytes))
	if err != nil {
		return SyncSummary{}, err
	}
	summary.summaryID = summaryID

	return summary, nil
}

func (s SyncSummary) Bytes() []byte {
	return s.bytes
}

func (s SyncSummary) Height() uint64 {
	return s.BlockNumber
}

func (s SyncSummary) ID() ids.ID {
	return s.summaryID
}

func (s SyncSummary) String() string {
	return fmt.Sprintf(
		"SyncSummary(BlockID=%s, BlockNumber=%d, BlockRoot=%s)",
		s.BlockID, s.BlockNumber, s.BlockRoot,
	)
}

func (s SyncSummary) Accept(context.Context) (block.StateSyncMode, error) {
	return s.acceptFunc(s)
}
