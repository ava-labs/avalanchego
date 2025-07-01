// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type ChangeNotifier struct {
	block.ChainVM
	OnChange func()
}

func (cn *ChangeNotifier) SetPreference(ctx context.Context, blkID ids.ID) error {
	defer cn.OnChange()
	return cn.ChainVM.SetPreference(ctx, blkID)
}

func (cn *ChangeNotifier) SetState(ctx context.Context, state snow.State) error {
	defer cn.OnChange()
	return cn.ChainVM.SetState(ctx, state)
}

func (cn *ChangeNotifier) BuildBlock(ctx context.Context) (snowman.Block, error) {
	defer cn.OnChange()
	return cn.ChainVM.BuildBlock(ctx)
}
