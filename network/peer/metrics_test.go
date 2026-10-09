// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/message"
)

func TestMetricsSentAndReceived(t *testing.T) {
	tests := []struct {
		name           string
		op             message.Op
		bytesSaved     int
		wantCompressed string
	}{
		{
			name:           "uncompressed",
			op:             message.ChitsOp,
			bytesSaved:     0,
			wantCompressed: "false",
		},
		{
			name:           "compressed",
			op:             message.PutOp,
			bytesSaved:     7,
			wantCompressed: "true",
		},
		{
			// An undefined op must take the fallback path rather than index
			// past the pre-resolved tables.
			name:           "undefined_op",
			op:             message.NumOps + 1,
			bytesSaved:     0,
			wantCompressed: "false",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			m, err := NewMetrics(prometheus.NewRegistry())
			require.NoError(err)

			const (
				sentBytes     = 10
				receivedBytes = 20
			)
			op := tt.op.String()
			outbound := &message.OutboundMessage{
				Op:                    tt.op,
				Bytes:                 make([]byte, sentBytes),
				BytesSavedCompression: tt.bytesSaved,
			}
			inbound := &message.InboundMessage{
				Op:                    tt.op,
				BytesSavedCompression: tt.bytesSaved,
			}

			m.Sent(outbound)
			m.Sent(outbound)
			m.Received(inbound, receivedBytes)
			m.SendFailed(outbound)
			m.MultipleSendsFailed(tt.op, 3)

			require.Equal(float64(2), testutil.ToFloat64(m.Messages.WithLabelValues(sentLabel, op, tt.wantCompressed)))
			require.Equal(float64(2*sentBytes), testutil.ToFloat64(m.Bytes.WithLabelValues(sentLabel, op)))
			require.Equal(float64(2*tt.bytesSaved), testutil.ToFloat64(m.BytesSaved.WithLabelValues(sentLabel, op)))

			require.Equal(float64(1), testutil.ToFloat64(m.Messages.WithLabelValues(receivedLabel, op, tt.wantCompressed)))
			require.Equal(float64(receivedBytes), testutil.ToFloat64(m.Bytes.WithLabelValues(receivedLabel, op)))
			require.Equal(float64(tt.bytesSaved), testutil.ToFloat64(m.BytesSaved.WithLabelValues(receivedLabel, op)))

			require.Equal(float64(4), testutil.ToFloat64(m.NumSendFailed.WithLabelValues(op)))
		})
	}
}

func BenchmarkMetricsReceived(b *testing.B) {
	m, err := NewMetrics(prometheus.NewRegistry())
	require.NoError(b, err)
	msg := &message.InboundMessage{Op: message.ChitsOp}

	b.ReportAllocs()
	for b.Loop() {
		m.Received(msg, 100)
	}
}

func BenchmarkMetricsSent(b *testing.B) {
	m, err := NewMetrics(prometheus.NewRegistry())
	require.NoError(b, err)
	msg := &message.OutboundMessage{Op: message.ChitsOp, Bytes: make([]byte, 100)}

	b.ReportAllocs()
	for b.Loop() {
		m.Sent(msg)
	}
}
