// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

// TODO improve/add comments.
// Shared fields used by visitors.
type backend struct {
	mempool.Mempool
	blkIDToState map[ids.ID]*blockState
	state        state.State
	ctx          *snow.Context
	bootstrapped *utils.AtomicBool
}

func (b *backend) OnAccept(blkID ids.ID) state.Chain {
	blockState, ok := b.blkIDToState[blkID]
	if !ok {
		return b.state
	}
	return blockState.onAcceptState
}

func (b *backend) free(blkID ids.ID) {
	delete(b.blkIDToState, blkID)
}
