// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

// LeafRecorder counts [evmstate.Stats] invocations.
type LeafRecorder struct {
	evmstate.NoopStats
	Requests       uint32
	Invalid        uint32
	MissingRoot    uint32
	TrieErrors     uint32
	ProofErrors    uint32
	SnapAttempts   uint32
	SnapSuccess    uint32
	SnapErrors     uint32
	SegmentValid   uint32
	SegmentInvalid uint32
	LeafReturned   uint16
}

func (s *LeafRecorder) IncLeafRequest()             { s.Requests++ }
func (s *LeafRecorder) IncInvalidLeafRequest()      { s.Invalid++ }
func (s *LeafRecorder) IncMissingRoot()             { s.MissingRoot++ }
func (s *LeafRecorder) IncTrieError()               { s.TrieErrors++ }
func (s *LeafRecorder) IncProofError()              { s.ProofErrors++ }
func (s *LeafRecorder) IncSnapshotReadAttempt()     { s.SnapAttempts++ }
func (s *LeafRecorder) IncSnapshotReadSuccess()     { s.SnapSuccess++ }
func (s *LeafRecorder) IncSnapshotReadError()       { s.SnapErrors++ }
func (s *LeafRecorder) IncSnapshotSegmentValid()    { s.SegmentValid++ }
func (s *LeafRecorder) IncSnapshotSegmentInvalid()  { s.SegmentInvalid++ }
func (s *LeafRecorder) UpdateLeafReturned(n uint16) { s.LeafReturned = n }

// CodeRecorder counts [code.Stats] invocations.
type CodeRecorder struct {
	code.NoopStats
	Requests          uint32
	MissingHash       uint32
	TooManyHashes     uint32
	DuplicateHashes   uint32
	CodeBytesReturned uint32
}

func (s *CodeRecorder) IncCodeRequest()                  { s.Requests++ }
func (s *CodeRecorder) IncMissingCodeHash()              { s.MissingHash++ }
func (s *CodeRecorder) IncTooManyHashesRequested()       { s.TooManyHashes++ }
func (s *CodeRecorder) IncDuplicateHashesRequested()     { s.DuplicateHashes++ }
func (s *CodeRecorder) UpdateCodeBytesReturned(b uint32) { s.CodeBytesReturned += b }

// BlockRecorder counts [block.Stats] invocations.
type BlockRecorder struct {
	block.NoopStats
	Requests       uint32
	MissingHash    uint32
	BlocksReturned uint16
}

func (s *BlockRecorder) IncBlockRequest()              { s.Requests++ }
func (s *BlockRecorder) IncMissingBlockHash()          { s.MissingHash++ }
func (s *BlockRecorder) UpdateBlocksReturned(n uint16) { s.BlocksReturned = n }
