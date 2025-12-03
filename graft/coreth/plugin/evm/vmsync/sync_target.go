// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ message.Syncable = (*syncTarget)(nil)

// syncTarget is a minimal implementation of [message.Syncable] used internally
// to advance the coordinator's sync target from engine-accepted blocks.
//
// NOTE: Unlike [message.BlockSyncSummary], this is not serializable and should not
// be used for network communication. Only [message.Syncable.GetBlockHash],
// [message.Syncable.GetBlockRoot], and [message.Syncable.Height] are used in practice.
// The other methods are stubs to satisfy the interface.
type syncTarget struct {
	hash   common.Hash
	root   common.Hash
	height uint64
}

func newSyncTarget(hash common.Hash, root common.Hash, height uint64) message.Syncable {
	return &syncTarget{hash: hash, root: root, height: height}
}

func (s *syncTarget) GetBlockHash() common.Hash { return s.hash }
func (s *syncTarget) GetBlockRoot() common.Hash { return s.root }

func (s *syncTarget) ID() ids.ID     { return ids.ID(s.hash) }
func (s *syncTarget) Height() uint64 { return s.height }
func (s *syncTarget) Bytes() []byte  { return s.hash.Bytes() }
func (*syncTarget) Accept(context.Context) (block.StateSyncMode, error) {
	// When used internally to advance targets, we always handle dynamically.
	return block.StateSyncDynamic, nil
}
