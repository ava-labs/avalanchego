// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ block.StateSummary = &SyncSummary{}

// SyncSummary provides the information necessary to sync a node starting
// at the given block.
type SyncSummary struct {
	BlockNumber uint64      `serialize:"true"`
	BlockHash   common.Hash `serialize:"true"`
	BlockRoot   common.Hash `serialize:"true"`
	AtomicRoot  common.Hash `serialize:"true"`

	summaryID  ids.ID
	bytes      []byte
	acceptImpl func(SyncSummary) (bool, error)
}

func NewSyncSummaryFromBytes(summaryBytes []byte, acceptImpl func(SyncSummary) (bool, error)) (SyncSummary, error) {
	summary := SyncSummary{}
	if codecVersion, err := Codec.Unmarshal(summaryBytes, &summary); err != nil {
		return SyncSummary{}, err
	} else if codecVersion != Version {
		return SyncSummary{}, fmt.Errorf("failed to parse syncable summary due to unexpected codec version (%d != %d)", codecVersion, Version)
	}

	summary.bytes = summaryBytes
	summaryID, err := ids.ToID(crypto.Keccak256(summaryBytes))
	if err != nil {
		return SyncSummary{}, err
	}
	summary.summaryID = summaryID
	summary.acceptImpl = acceptImpl
	return summary, nil
}

func NewSyncSummary(blockHash common.Hash, blockNumber uint64, blockRoot common.Hash, atomicRoot common.Hash) (SyncSummary, error) {
	summary := SyncSummary{
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		BlockRoot:   blockRoot,
		AtomicRoot:  atomicRoot,
	}
	bytes, err := Codec.Marshal(Version, &summary)
	if err != nil {
		return SyncSummary{}, err
	}

	summary.bytes = bytes
	summaryID, err := ids.ToID(crypto.Keccak256(bytes))
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
	return fmt.Sprintf("SyncSummary(BlockHash=%s, BlockNumber=%d, BlockRoot=%s, AtomicRoot=%s)", s.BlockHash, s.BlockNumber, s.BlockRoot, s.AtomicRoot)
}

func (s SyncSummary) Accept() (bool, error) {
	if s.acceptImpl == nil {
		return false, fmt.Errorf("accept implementation not specified for summary: %s", s)
	}
	return s.acceptImpl(s)
}
