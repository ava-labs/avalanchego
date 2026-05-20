// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
)

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
