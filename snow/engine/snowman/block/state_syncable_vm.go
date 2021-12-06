// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// Default Network VMs have a specific structure for Summary.Key
// However StateSyncableVM does not require usage of these default formats
const StateSyncDefaultKeysVersion = 0

type ProposerSummaryKey struct {
	ProBlkID ids.ID            `serialize:"true"`
	InnerKey DefaultSummaryKey `serialize:"true"`
}

type DefaultSummaryKey struct {
	InnerBlkID   ids.ID `serialize:"true"`
	InnerVMBytes []byte `serialize:"true"`
}

type StateSyncableVM interface {
	common.StateSyncableVM

	// At the end of StateSync process, VM will have rebuilt the state of its blockchain
	// up to a given height. However the block associated with that height may be not known
	// to the VM yet. GetLastSummaryBlockID allows retrival of this block from network
	GetLastSummaryBlockID() (ids.ID, error)

	// SetLastSummaryBlock pass to VM the network-retrieved block associated with its last state summary
	SetLastSummaryBlock([]byte) error
}
