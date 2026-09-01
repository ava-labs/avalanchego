// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"testing"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func withTracer(tr oteltrace.Tracer) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.tracer = tr
	})
}

func TestExecutionTracing(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer("test")

	ctx, sut := newSUT(t, withTracer(tracer))

	enqueueCtx, enqueueSpan := tracer.Start(ctx, "test.enqueue")
	b := sut.chain.NewBlock(t, nil)
	require.NoError(t, sut.Enqueue(enqueueCtx, b), "Enqueue()")
	enqueueSpan.End()

	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	// The execution span only ends after the block is marked as executed, so
	// close the Executor to guarantee that execution has fully finished.
	require.NoErrorf(t, sut.Close(), "%T.Close()", sut)

	spans := recorder.Ended()
	var execSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "saevm.executor.execute_block" {
			execSpan = s
			break
		}
	}
	require.NotNil(t, execSpan, "span %q must be recorded after block execution", "saevm.executor.execute_block")

	links := execSpan.Links()
	require.Len(t, links, 1, "execute_block span links")
	require.Equal(t, enqueueSpan.SpanContext(), links[0].SpanContext, "execute_block span must link to the span active at Enqueue() time")

	attrs := attribute.NewSet(execSpan.Attributes()...)
	gotHeight, ok := attrs.Value("saevm.block.height")
	require.True(t, ok, "execute_block span attribute saevm.block.height")
	require.Equal(t, int64(1), gotHeight.AsInt64(), "saevm.block.height attribute")
	_, ok = attrs.Value("saevm.block.hash")
	require.True(t, ok, "execute_block span attribute saevm.block.hash")

	wantChildren := []string{
		"saevm.hook.start_executing_block",
		"saevm.execute.transactions",
		"saevm.hook.end_of_block_ops",
		"saevm.hook.finish_executing_block",
		"saevm.hook.after_executing_block",
		"saevm.executor.commit",
	}
	var gotChildren []string
	for _, s := range spans {
		if s.Parent().SpanID() == execSpan.SpanContext().SpanID() {
			gotChildren = append(gotChildren, s.Name())
		}
	}
	require.Subset(t, gotChildren, wantChildren, "execute_block child spans")
}
