// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ common.Summary = &Summary{}

const StateSyncDefaultKeysVersion = 0

type Summary struct {
	SummaryKey   common.SummaryKey
	SummaryHash  common.SummaryHash
	ContentBytes []byte
}

func (s *Summary) Bytes() []byte            { return s.ContentBytes }
func (s *Summary) Key() common.SummaryKey   { return s.SummaryKey }
func (s *Summary) Hash() common.SummaryHash { return s.SummaryHash }

type CoreSummaryContent struct {
	BlkID   ids.ID `serialize:"true"`
	Height  uint64 `serialize:"true"`
	Content []byte `serialize:"true"`
}

type ProposerSummaryContent struct {
	ProBlkID    ids.ID             `serialize:"true"`
	CoreContent CoreSummaryContent `serialize:"true"`
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
