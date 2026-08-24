// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/saevm/saetrace"
)

// tracerName identifies this package as the instrumentation scope of the spans
// it records.
const tracerName = "github.com/ava-labs/avalanchego/vms/saevm/saexec"

// Span names recorded by this package. The executor records one
// [spanExecuteBlock] per block, linked to the span (if any) that was active
// when the block was enqueued; all others are its children.
const (
	spanExecuteBlock         = "saevm.executor.execute_block"
	spanCommit               = "saevm.executor.commit"
	spanExecuteTransactions  = "saevm.execute.transactions"
	spanStartExecutingBlock  = "saevm.hook.start_executing_block"
	spanEndOfBlockOps        = "saevm.hook.end_of_block_ops"
	spanFinishExecutingBlock = "saevm.hook.finish_executing_block"
	spanAfterExecutingBlock  = "saevm.hook.after_executing_block"
)

// traced runs fn in a child span of ctx with the given name, recording any
// returned error on the span.
func traced(ctx context.Context, name string, fn func(context.Context) error) error {
	return saetrace.Traced(ctx, tracerName, name, fn)
}
