// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// mockRequester returns one scripted avax.getAtomicTx response per call.
type mockRequester struct {
	responses []mockResponse
	calls     int
}

type mockResponse struct {
	height *uint64
	err    error
}

func (m *mockRequester) SendRequest(_ context.Context, method string, _ interface{}, reply interface{}, _ ...rpc.Option) error {
	if method != "avax.getAtomicTx" {
		return errors.New("unexpected method " + method)
	}
	r := m.responses[min(m.calls, len(m.responses)-1)]
	m.calls++
	if r.err != nil {
		return r.err
	}
	if r.height != nil {
		h := json.Uint64(*r.height)
		reply.(*getAtomicTxHeightReply).BlockHeight = &h
	}
	return nil
}

func TestAwaitTxAccepted(t *testing.T) {
	height := uint64(42)
	errUnavailable := errors.New("the method avax.getAtomicTx is not available")
	tests := []struct {
		name      string
		responses []mockResponse
		wantErr   error
		wantCalls int
	}{
		{
			name:      "accepted",
			responses: []mockResponse{{height: &height}},
			wantCalls: 1,
		},
		{
			name: "sae_not_found_then_accepted",
			responses: []mockResponse{
				{err: errors.New("fetching tx: reading tx: not found")},
				{height: &height},
			},
			wantCalls: 2,
		},
		{
			name: "coreth_unknown_processing_then_accepted",
			responses: []mockResponse{
				{err: errors.New("could not find tx 2QouvFWUbjuySRxeX5xMbNCuAaKWfbk5FeEa2JmoF85RKLk2dD")},
				{},
				{height: &height},
			},
			wantCalls: 3,
		},
		{
			name:      "other_error",
			responses: []mockResponse{{err: errUnavailable}},
			wantErr:   errUnavailable,
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m := &mockRequester{responses: tt.responses}
			c := &Client{requester: m}
			err := c.AwaitTxAccepted(context.Background(), ids.GenerateTestID(), time.Millisecond)
			require.ErrorIs(err, tt.wantErr)
			require.Equal(tt.wantCalls, m.calls)
		})
	}
}

func TestAwaitTxAcceptedContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &mockRequester{responses: []mockResponse{{err: errors.New("fetching tx: reading tx: not found")}}}
	c := &Client{requester: m}
	err := c.AwaitTxAccepted(ctx, ids.GenerateTestID(), time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
}
