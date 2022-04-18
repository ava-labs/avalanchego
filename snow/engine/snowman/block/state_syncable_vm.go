// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

type StateSyncableVM interface {
	common.StateSyncableVM

	// VM State Sync process must run asynchronously; moreover, once it is done,
	// the full block associated with synced summary must be downloaded from
	// the network. StateSyncGetResult returns:
	// 1- height and ID of this block to allow its retrival from network
	// 2- error state of the whole StateSync process so far
	StateSyncGetResult() (ids.ID, uint64, error)

	// Once last summary block pulled from VM via StateSyncGetResult has been
	// retrieved from network and validated, StateSyncSetLastSummaryBlockID
	// confirms it to the VM.
	// StateSyncSetLastSummaryBlockID is preceded by a ParseBlock call which
	// is assumed to store the parse block upon success
	StateSyncSetLastSummaryBlockID(blkID ids.ID) error
}
