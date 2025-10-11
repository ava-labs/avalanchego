// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	oteltrace "go.opentelemetry.io/otel/trace"
)

func (vm *blockVM) SetPreferenceWithContext(ctx context.Context, blkID ids.ID, blockCtx *block.Context) error {
	if vm.setPreferenceVM == nil || blockCtx == nil {
		return vm.SetPreference(ctx, blkID)
	}

	ctx, span := vm.tracer.Start(ctx, vm.setPreferenceWithContextTag, oteltrace.WithAttributes(
		attribute.Stringer("blkID", blkID),
		attribute.Int64("pChainHeight", int64(blockCtx.PChainHeight)),
	))
	defer span.End()

	return vm.setPreferenceVM.SetPreferenceWithContext(ctx, blkID, blockCtx)
}
