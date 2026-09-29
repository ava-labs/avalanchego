// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// txResult is the scripted reply to a single GetTx call.
type txResult struct {
	height uint64
	err    error
}

// scriptedTxGetter returns one scripted result per GetTx call and repeats the
// last result once the script runs out.
type scriptedTxGetter struct {
	results []txResult
	calls   int
}

func (s *scriptedTxGetter) GetTx(context.Context, ids.ID, ...rpc.Option) (*tx.Tx, uint64, error) {
	r := s.results[min(s.calls, len(s.results)-1)]
	s.calls++
	return nil, r.height, r.err
}

func TestAwaitTxAccepted(t *testing.T) {
	errNotFound := fmt.Errorf("sending request: fetching tx: reading tx: %w", database.ErrNotFound)
	errUnavailable := errors.New("the method avax.getAtomicTx is not available")
	tests := []struct {
		name      string
		results   []txResult
		wantErr   error
		wantCalls int
	}{
		{
			name:      "accepted",
			results:   []txResult{{height: 1}},
			wantCalls: 1,
		},
		{
			name: "not_found_then_accepted",
			results: []txResult{
				{err: errNotFound},
				{height: 1},
			},
			wantCalls: 2,
		},
		{
			name: "processing_then_accepted",
			results: []txResult{
				{height: 0},
				{height: 1},
			},
			wantCalls: 2,
		},
		{
			name:      "other_error",
			results:   []txResult{{err: errUnavailable}},
			wantErr:   errUnavailable,
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			g := &scriptedTxGetter{results: tt.results}
			err := awaitTxAccepted(t.Context(), g, ids.GenerateTestID(), time.Millisecond)
			require.ErrorIs(err, tt.wantErr)
			require.Equal(tt.wantCalls, g.calls)
		})
	}
}

func TestAwaitTxAcceptedContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	g := &scriptedTxGetter{
		results: []txResult{{err: fmt.Errorf("sending request: fetching tx: reading tx: %w", database.ErrNotFound)}},
	}
	err := awaitTxAccepted(ctx, g, ids.GenerateTestID(), time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
}
