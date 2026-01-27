// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	if vm.setPreferenceVM == nil {
		return vm.SetPreference(ctx, blkID)
	}

	var option oteltrace.SpanStartEventOption
	if blockCtx == nil {
		option = oteltrace.WithAttributes(
			attribute.Stringer("blkID", blkID),
		)
	} else {
		option = oteltrace.WithAttributes(
			attribute.Stringer("blkID", blkID),
			attribute.Int64("pChainHeight", int64(blockCtx.PChainHeight)),
		)
	}

	ctx, span := vm.tracer.Start(ctx, vm.setPreferenceWithContextTag, option)
	defer span.End()

	return vm.setPreferenceVM.SetPreferenceWithContext(ctx, blkID, blockCtx)
}
