// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// VMAdapter provides VM-specific operations needed for state sync
type VMAdapter interface {
	// Core VM state access
	GetChainState() (ChainState, error)
	GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error)
	ProcessSyncedBlock(block BlockWrapper) error
	SetLastAcceptedBlock(block snowman.Block) error
	UpdateChainPointers(summary StateSummary) error

	// Database operations
	WipeSnapshot() error
	ResetSnapshotGenerator() error
}

// ChainState represents VM chain state
type ChainState struct {
	LastAcceptedHeight uint64
	ChainDB            ethdb.Database
	VerDB              *versiondb.Database
	MetadataDB         database.Database
	State              interface{} // VM-specific state interface
}

// BlockWrapper abstracts VM-specific block types
type BlockWrapper interface {
	GetEthBlock() *types.Block
}

// StateSummary abstracts VM-specific summary types
type StateSummary interface {
	GetBlockHash() common.Hash
	Height() uint64
	GetBlockRoot() common.Hash
	Bytes() []byte
}

// BlockChain abstracts blockchain operations needed by the sync server
type BlockChain interface {
	GetBlockByNumber(num uint64) *types.Block
	HasState(root common.Hash) bool
	LastAcceptedBlock() *types.Block
}
