// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/saevm/saetrace"
)

// tracerName identifies this package as the instrumentation scope of the spans
// it records.
const tracerName = "github.com/ava-labs/avalanchego/vms/saevm/sae"

// Span names recorded by this package. [spanBuildBlock] covers all of
// [blockBuilderG.buildWithTxs] (block building and rebuilding); the others are
// its children.
const (
	spanBuildBlock            = "saevm.builder.build_block"
	spanWorstcaseReplay       = "saevm.builder.worstcase_replay"
	spanSelectTransactions    = "saevm.builder.select_transactions"
	spanBuildHeaderHook       = "saevm.hook.build_header"
	spanPotentialEndOfBlockOp = "saevm.hook.potential_end_of_block_ops"
	spanBuildBlockHook        = "saevm.hook.build_block"
)

// traced runs fn in a child span of ctx with the given name, recording any
// returned error on the span.
func traced(ctx context.Context, name string, fn func(context.Context) error) error {
	return saetrace.Traced(ctx, tracerName, name, fn)
}
