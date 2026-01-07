// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) SetPreferenceWithContext(ctx context.Context, blkID ids.ID, blockCtx *block.Context) error {
	if vm.setPreferenceVM == nil {
		return vm.SetPreference(ctx, blkID)
	}

	start := time.Now()
	err := vm.setPreferenceVM.SetPreferenceWithContext(ctx, blkID, blockCtx)
	vm.blockMetrics.setPreferenceWithContext.Observe(float64(time.Since(start)))
	return err
}
