// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/ids"

	snowblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ message.Syncable = (*SyncTarget)(nil)

// SyncTarget is a minimal message.Syncable implementation for tests.
type SyncTarget struct {
	BlockHash   common.Hash
	BlockRoot   common.Hash
	BlockHeight uint64
}

func (s *SyncTarget) GetBlockHash() common.Hash                             { return s.BlockHash }
func (s *SyncTarget) GetBlockRoot() common.Hash                             { return s.BlockRoot }
func (s *SyncTarget) ID() ids.ID                                            { return ids.ID(s.BlockHash) }
func (s *SyncTarget) Height() uint64                                        { return s.BlockHeight }
func (s *SyncTarget) Bytes() []byte                                         { return s.BlockHash.Bytes() }
func (*SyncTarget) Accept(context.Context) (snowblock.StateSyncMode, error) { return 0, nil }
