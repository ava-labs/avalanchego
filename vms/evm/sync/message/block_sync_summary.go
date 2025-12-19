// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ Syncable       = (*BlockSyncSummary)(nil)
	_ SyncableParser = (*BlockSyncSummaryParser)(nil)

	// errInvalidBlockSyncSummary is returned when the provided bytes cannot be
	// parsed into a valid BlockSyncSummary.
	errInvalidBlockSyncSummary = errors.New("invalid block sync summary")

	// errAcceptImplNotSpecified is returned when Accept is called on a BlockSyncSummary
	// that doesn't have an acceptImpl set.
	errAcceptImplNotSpecified = errors.New("accept implementation not specified")
)

// Syncable extends [block.StateSummary] with EVM-specific block information.
type Syncable interface {
	block.StateSummary
	GetBlockHash() common.Hash
	GetBlockRoot() common.Hash
}

// SyncableParser parses raw bytes into a [Syncable] instance.
type SyncableParser interface {
	Parse(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error)
}

// AcceptImplFn is a function that determines the state sync mode for a given [Syncable].
type AcceptImplFn func(Syncable) (block.StateSyncMode, error)

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

// NewBlockSyncSummary creates a new [BlockSyncSummary] for the given block.
// The acceptImpl is intentionally left unset and should be set by the parser.
func NewBlockSyncSummary(blockHash common.Hash, blockNumber uint64, blockRoot common.Hash) (*BlockSyncSummary, error) {
	summary := BlockSyncSummary{
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		BlockRoot:   blockRoot,
	}
	bytes, err := Codec().Marshal(Version, &summary)
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
		return block.StateSyncSkipped, errAcceptImplNotSpecified
	}
	return s.acceptImpl(s)
}

// BlockSyncSummaryParser parses [BlockSyncSummary] instances from raw bytes.
type BlockSyncSummaryParser struct{}

// NewBlockSyncSummaryParser creates a new [BlockSyncSummaryParser].
func NewBlockSyncSummaryParser() *BlockSyncSummaryParser {
	return &BlockSyncSummaryParser{}
}

func (*BlockSyncSummaryParser) Parse(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error) {
	summary := BlockSyncSummary{}
	if _, err := Codec().Unmarshal(summaryBytes, &summary); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidBlockSyncSummary, err)
	}

	summary.bytes = summaryBytes
	summaryID, err := ids.ToID(crypto.Keccak256(summaryBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to compute summary ID: %w", err)
	}
	summary.summaryID = summaryID
	summary.acceptImpl = acceptImpl
	return &summary, nil
}

// BlockSyncSummaryProvider provides state summaries for blocks.
type BlockSyncSummaryProvider struct{}

// StateSummaryAtBlock returns the block state summary for the given block if valid.
func (*BlockSyncSummaryProvider) StateSummaryAtBlock(blk *types.Block) (block.StateSummary, error) {
	return NewBlockSyncSummary(blk.Hash(), blk.NumberU64(), blk.Root())
}
