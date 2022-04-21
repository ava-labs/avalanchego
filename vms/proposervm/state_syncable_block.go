// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var _ snowman.StateSyncableBlock = &stateSyncableBlock{}

type stateSyncableBlock struct {
	Block
	innerStateSyncableBlk snowman.StateSyncableBlock
}

func (ssb *stateSyncableBlock) Register() error {
	if err := ssb.Block.acceptOuterBlk(); err != nil {
		return err
	}

	return ssb.innerStateSyncableBlk.Register()
}
