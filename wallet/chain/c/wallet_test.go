// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// scriptedTxGetter returns one scripted error per GetTx call. A nil error
// means the tx is accepted.
type scriptedTxGetter struct {
	errs  []error
	calls int
}

func (s *scriptedTxGetter) GetTx(context.Context, ids.ID, ...rpc.Option) (*tx.Tx, uint64, error) {
	err := s.errs[min(s.calls, len(s.errs)-1)]
	s.calls++
	return nil, 0, err
}

func TestAwaitTxAccepted(t *testing.T) {
	errUnavailable := errors.New("the method avax.getAtomicTx is not available")
	tests := []struct {
		name      string
		errs      []error
		wantErr   error
		wantCalls int
	}{
		{
			name:      "accepted",
			errs:      []error{nil},
			wantCalls: 1,
		},
		{
			name: "sae_not_found_then_accepted",
			errs: []error{
				errors.New("sending request: fetching tx: reading tx: not found"),
				nil,
			},
			wantCalls: 2,
		},
		{
			name: "coreth_not_found_then_accepted",
			errs: []error{
				errors.New("sending request: could not find tx 2QouvFWUbjuySRxeX5xMbNCuAaKWfbk5FeEa2JmoF85RKLk2dD"),
				nil,
			},
			wantCalls: 2,
		},
		{
			name:      "other_error",
			errs:      []error{errUnavailable},
			wantErr:   errUnavailable,
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			g := &scriptedTxGetter{errs: tt.errs}
			err := awaitTxAccepted(t.Context(), g, ids.GenerateTestID(), time.Millisecond)
			require.ErrorIs(err, tt.wantErr)
			require.Equal(tt.wantCalls, g.calls)
		})
	}
}

func TestAwaitTxAcceptedContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	g := &scriptedTxGetter{errs: []error{errors.New("fetching tx: reading tx: not found")}}
	err := awaitTxAccepted(ctx, g, ids.GenerateTestID(), time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
}
