// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

type StateSyncableVM interface {
	common.StateSyncableVM

	// At the end of StateSync process, VM will have rebuilt the state of its blockchain
	// up to a given height. However the block associated with that height may be not known
	// to the VM yet. GetStateSyncResult allows retrival of this block from network
	GetStateSyncResult() (ids.ID, uint64, error)

	// SetLastSummaryBlock pass to VM the network-retrieved block associated with its last state summary
	SetLastSummaryBlock([]byte) error
}
