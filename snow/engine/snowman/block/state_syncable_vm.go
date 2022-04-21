// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

type StateSyncableVM interface {
	common.StateSyncableVM

	// VM State Sync process must run asynchronously; moreover, once it is done,
	// the full block associated with synced summary must be downloaded from
	// the network. GetStateSyncResult returns:
	// 1- height and ID of this block to allow its retrival from network
	// 2- error state of the whole StateSync process so far
	GetStateSyncResult() (ids.ID, uint64, error)

	// Once block associated with state summary has been downloaded from the network
	// ParseStateSyncableBlock helps parsing it
	ParseStateSyncableBlock(blkBytes []byte) (snowman.StateSyncableBlock, error)
}
