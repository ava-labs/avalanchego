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

// syncTarget is a minimal implementation of message.Syncable used internally
// to advance the coordinator's sync target from engine-accepted blocks.
type syncTarget struct {
	id     ids.ID
	hash   common.Hash
	root   common.Hash
	height uint64
}

// Build a sync target from basic fields.
func newSyncTarget(hash common.Hash, root common.Hash, height uint64) message.Syncable {
	var id ids.ID
	copy(id[:], hash[:])
	return &syncTarget{
		id:     id,
		hash:   hash,
		root:   root,
		height: height,
	}
}

// message.Syncable
func (s *syncTarget) GetBlockHash() common.Hash { return s.hash }
func (s *syncTarget) GetBlockRoot() common.Hash { return s.root }

// block.StateSummary
func (s *syncTarget) ID() ids.ID     { return s.id }
func (s *syncTarget) Height() uint64 { return s.height }
func (s *syncTarget) Bytes() []byte  { return s.hash.Bytes() }
func (*syncTarget) Accept(context.Context) (block.StateSyncMode, error) {
	// When used internally to advance targets, we always handle dynamically.
	return block.StateSyncDynamic, nil
}
