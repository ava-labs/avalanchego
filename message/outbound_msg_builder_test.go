// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/compression"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func Test_newOutboundBuilder(t *testing.T) {
	t.Parallel()

	mb, err := newMsgBuilder(
		prometheus.NewRegistry(),
		10*time.Second,
		false,
	)
	require.NoError(t, err)

	for _, compressionType := range []compression.Type{
		compression.TypeNone,
		compression.TypeZstd,
	} {
		t.Run(compressionType.String(), func(t *testing.T) {
			builder := newOutboundBuilder(compressionType, mb)

			outMsg, err := builder.GetAcceptedStateSummary(
				t.Context(),
				ids.GenerateTestID(),
				12345,
				time.Hour,
				[]uint64{1000, 2000},
			)
			require.NoError(t, err)
			t.Logf("outbound message with compression type %s built message with size %d", compressionType, len(outMsg.Bytes))
		})
	}
}

func TestOutboundTraceContextFromCtx(t *testing.T) {
	mb, err := newMsgBuilder(prometheus.NewRegistry(), time.Second, true)
	require.NoError(t, err, "newMsgBuilder()")
	b := newOutboundBuilder(compression.TypeNone, mb)

	ctx, span := sdktrace.NewTracerProvider().Tracer("t").Start(t.Context(), "root")
	defer span.End()

	out, err := b.Chits(ctx, ids.GenerateTestID(), 1, ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID(), 3)
	require.NoError(t, err, "Chits()")
	var m p2p.Message
	require.NoError(t, proto.Unmarshal(out.Bytes, &m), "proto.Unmarshal()")
	require.Equal(t, trace.MarshalSpanContext(span.SpanContext()), m.GetTraceContext(), "TraceContext")

	out, err = b.Chits(t.Context(), ids.GenerateTestID(), 1, ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID(), 3)
	require.NoError(t, err, "Chits() without span")
	m.Reset()
	require.NoError(t, proto.Unmarshal(out.Bytes, &m), "proto.Unmarshal()")
	require.Empty(t, m.GetTraceContext(), "TraceContext without span")
}
