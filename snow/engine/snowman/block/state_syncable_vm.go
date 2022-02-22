// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

const StateSyncDefaultKeysVersion = 0

type CoreSummaryKey struct {
	Height      uint64 `serialize:"true"`
	SummaryHash ids.ID `serialize:"true"`
}

type CoreSummaryContent struct {
	BlkID   ids.ID `serialize:"true"`
	Height  uint64 `serialize:"true"`
	Content []byte `serialize:"true"`
}

type ProposerSummaryKey struct {
	Height         uint64 `serialize:"true"`
	ProSummaryHash ids.ID `serialize:"true"`
}

type ProposerSummaryContent struct {
	ProBlkID    ids.ID             `serialize:"true"`
	CoreContent CoreSummaryContent `serialize:"true"`
}

type StateSyncableVM interface {
	common.StateSyncableVM

	// StateSyncGetKeyHeight returns the height encoded into key.
	// It is an helper in the key validation process.
	StateSyncGetKeyHeight(common.Key) (uint64, error)

	// StateSyncGetSummary returns the summary associate with height if it is available.
	// It is an helper in the key validation process.
	StateSyncGetSummary(height uint64) (common.Summary, error)

	// At the end of StateSync process, VM will have rebuilt the state of its blockchain
	// up to a given height. However the block associated with that height may be not known
	// to the VM yet. GetLastSummaryBlockID allows retrival of this block from network
	GetLastSummaryBlockID() (ids.ID, error)

	// SetLastSummaryBlock pass to VM the network-retrieved block associated with its last state summary
	SetLastSummaryBlock([]byte) error
}
