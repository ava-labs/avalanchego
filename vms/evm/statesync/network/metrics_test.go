// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/peertracker/peertrackertest"

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

// TestDispatcherMetrics pins how each attempt is counted: exactly one of
// failed, invalid_response, or succeeded, with the latency observed for every
// accepted response.
func TestDispatcherMetrics(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	errRejected := errors.New("rejected")

	okBytes, err := proto.Marshal(&syncpb.GetLeafResponse{Keys: [][]byte{{1}, {2}, {3}}})
	require.NoError(t, err, "proto.Marshal()")
	reqBytes, err := proto.Marshal(&syncpb.GetLeafRequest{})
	require.NoError(t, err, "proto.Marshal()")

	tests := []struct {
		name    string
		handler p2p.Handler
		parse   func(*syncpb.GetLeafResponse) (*syncpb.GetLeafResponse, error)
		wantErr error
		want    map[string]float64
		wantObs uint64
	}{
		{
			name:    "success",
			handler: echoHandler(okBytes),
			parse:   acceptLeaf,
			want: map[string]float64{
				"requested": 1, "succeeded": 1, "failed": 0, "invalid_response": 0,
			},
			wantObs: 1,
		},
		{
			name:    "handler_error_is_failed",
			handler: errorHandler(),
			parse:   acceptLeaf,
			wantErr: errHandlerFailed,
			want: map[string]float64{
				"requested": 1, "succeeded": 0, "failed": 1, "invalid_response": 0,
			},
		},
		{
			name:    "garbage_response_is_invalid",
			handler: echoHandler([]byte{0xff, 0xff, 0xff}),
			parse:   acceptLeaf,
			wantErr: errUnmarshalResponse,
			want: map[string]float64{
				"requested": 1, "succeeded": 0, "failed": 0, "invalid_response": 1,
			},
		},
		{
			name:    "rejected_response_is_invalid",
			handler: echoHandler(okBytes),
			parse: func(*syncpb.GetLeafResponse) (*syncpb.GetLeafResponse, error) {
				return nil, errRejected
			},
			wantErr: errRejected,
			want: map[string]float64{
				"requested": 1, "succeeded": 0, "failed": 0, "invalid_response": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			tracker := peertrackertest.New(t)
			c := newTestDispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)

			_, err := c.sendBytes(ctx, reqBytes, tt.parse)
			require.ErrorIsf(t, err, tt.wantErr, "%T.sendBytes()", c)

			m := c.metrics
			counters := map[string]prometheus.Counter{
				"requested":        m.requested,
				"succeeded":        m.succeeded,
				"failed":           m.failed,
				"invalid_response": m.invalidResponse,
			}
			for name, want := range tt.want {
				require.Equalf(t, want, testutil.ToFloat64(counters[name]), "counter %q", name)
			}
			require.Equal(t, tt.wantObs, latencyObservations(t, m), "request latency observations")
		})
	}
}
