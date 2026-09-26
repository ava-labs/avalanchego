// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	dto "github.com/prometheus/client_model/go"
)

// latencyObservations returns the number of request latencies recorded on m.
func latencyObservations(t *testing.T, m *Metrics) uint64 {
	t.Helper()
	var pb dto.Metric
	require.NoError(t, m.requestLatency.Write(&pb), "requestLatency.Write()")
	return pb.GetHistogram().GetSampleCount()
}

// TestDispatcherMetrics pins how each request outcome is counted: exactly one
// of failed, invalid_response, or succeeded per request, with the latency
// observed for every response that arrived.
func TestDispatcherMetrics(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	okBytes, err := proto.Marshal(&syncpb.GetLeafResponse{Keys: [][]byte{{1}, {2}, {3}}})
	require.NoError(t, err, "proto.Marshal()")

	tests := []struct {
		name    string
		handler p2p.Handler
		wantErr error          // non-nil when SendTo must error
		outcome func(*Outcome) // nil when SendTo must error
		want    map[string]float64
		wantObs uint64
	}{
		{
			name:    "success_with_received",
			handler: echoHandler(okBytes),
			outcome: func(o *Outcome) {
				o.Success()
				o.MarkReceived(3)
			},
			want: map[string]float64{
				"requested": 1, "succeeded": 1, "failed": 0, "invalid_response": 0, "received": 3,
			},
			wantObs: 1,
		},
		{
			name:    "handler_error_is_failed",
			handler: errorHandler(),
			wantErr: errHandlerFailed,
			want: map[string]float64{
				"requested": 1, "succeeded": 0, "failed": 1, "invalid_response": 0, "received": 0,
			},
			wantObs: 0,
		},
		{
			name:    "garbage_response_is_invalid",
			handler: echoHandler([]byte{0xff, 0xff, 0xff}),
			wantErr: errUnmarshalResponse,
			want: map[string]float64{
				"requested": 1, "succeeded": 0, "failed": 0, "invalid_response": 1, "received": 0,
			},
			wantObs: 1,
		},
		{
			name:    "rejected_response_is_invalid",
			handler: echoHandler(okBytes),
			outcome: func(o *Outcome) {
				o.Failure()
				o.Success() // idempotent: must not also count succeeded
			},
			want: map[string]float64{
				"requested": 1, "succeeded": 0, "failed": 0, "invalid_response": 1, "received": 0,
			},
			wantObs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			_, tracker := newTestTracker(t, nodeID)
			c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)

			outcome, err := c.SendTo(ctx, nodeID, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{})
			if tt.outcome == nil {
				require.ErrorIs(t, err, tt.wantErr, "SendTo()")
			} else {
				require.NoError(t, err, "SendTo()")
				tt.outcome(outcome)
			}

			m := c.metrics
			counters := map[string]prometheus.Counter{
				"requested":        m.requested,
				"succeeded":        m.succeeded,
				"failed":           m.failed,
				"invalid_response": m.invalidResponse,
				"received":         m.received,
			}
			for name, want := range tt.want {
				require.Equalf(t, want, testutil.ToFloat64(counters[name]), "counter %q", name)
			}
			require.Equal(t, tt.wantObs, latencyObservations(t, m), "request latency observations")
		})
	}
}
