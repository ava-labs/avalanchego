// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type Syncable interface {
	block.StateSummary
	GetBlockHash() common.Hash
	GetBlockRoot() common.Hash
}

type SyncableParser interface {
	Parse(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error)
}

type AcceptImplFn func(Syncable) (block.StateSyncMode, error)
