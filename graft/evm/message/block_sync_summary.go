// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ Syncable = (*BlockSyncSummary)(nil)

// BlockSyncSummary provides the information necessary to sync a node starting
// at the given block.
type BlockSyncSummary struct {
	BlockNumber uint64      `serialize:"true"`
	BlockHash   common.Hash `serialize:"true"`
	BlockRoot   common.Hash `serialize:"true"`

	summaryID  ids.ID
	bytes      []byte
	acceptImpl AcceptImplFn
}

func NewBlockSyncSummary(c codec.Manager, blockHash common.Hash, blockNumber uint64, blockRoot common.Hash) (*BlockSyncSummary, error) {
	// We intentionally do not use the acceptImpl here and leave it for the parser to set.
	summary := BlockSyncSummary{
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		BlockRoot:   blockRoot,
	}
	bytes, err := c.Marshal(Version, &summary)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal syncable summary: %w", err)
	}

	summary.bytes = bytes
	summaryID, err := ids.ToID(crypto.Keccak256(bytes))
	if err != nil {
		return nil, fmt.Errorf("failed to compute summary ID: %w", err)
	}
	summary.summaryID = summaryID

	return &summary, nil
}

func (s *BlockSyncSummary) GetBlockHash() common.Hash {
	return s.BlockHash
}

func (s *BlockSyncSummary) GetBlockRoot() common.Hash {
	return s.BlockRoot
}

func (s *BlockSyncSummary) Bytes() []byte {
	return s.bytes
}

func (s *BlockSyncSummary) Height() uint64 {
	return s.BlockNumber
}

func (s *BlockSyncSummary) ID() ids.ID {
	return s.summaryID
}

func (s *BlockSyncSummary) String() string {
	return fmt.Sprintf("BlockSyncSummary(BlockHash=%s, BlockNumber=%d, BlockRoot=%s)", s.BlockHash, s.BlockNumber, s.BlockRoot)
}

func (s *BlockSyncSummary) Accept(context.Context) (block.StateSyncMode, error) {
	if s.acceptImpl == nil {
		return block.StateSyncSkipped, fmt.Errorf("accept implementation not specified for summary: %s", s)
	}
	return s.acceptImpl(s)
}
