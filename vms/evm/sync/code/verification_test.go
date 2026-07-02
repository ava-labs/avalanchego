// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestGetCode_RetriesUntilValid(t *testing.T) {
	codeBytes := []byte{0x60, 0x00, 0x60, 0x00}
	codeHash := crypto.Keccak256Hash(codeBytes)

	goodResp := synctest.MustMarshal(t, &syncpb.GetCodeResponse{Data: [][]byte{codeBytes}})
	mismatchResp := synctest.MustMarshal(t, &syncpb.GetCodeResponse{Data: [][]byte{{0xde, 0xad}}})
	wrongLenResp := synctest.MustMarshal(t, &syncpb.GetCodeResponse{Data: [][]byte{codeBytes, codeBytes}})

	tests := []struct {
		name    string
		badResp []byte
	}{
		{
			name:    "wrong response length is retried",
			badResp: wrongLenResp,
		},
		{
			name:    "hash mismatch is retried",
			badResp: mismatchResp,
		},
		{
			name:    "network error is retried",
			badResp: nil, // handler returns an AppError
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// responses served in order: bad first, then good.
			h := synctest.NewScriptedHandler([][]byte{tt.badResp, goodResp})

			g := newTestGetter(t, h)

			codeSlices, err := g.GetCode(t.Context(), []common.Hash{codeHash})
			require.NoError(t, err)
			require.Len(t, codeSlices, 1)
			require.Equal(t, codeBytes, codeSlices[0])
			require.Equal(t, 2, h.Calls())
		})
	}
}

// TestGetCode_ContextCancelled asserts that a cancelled context returns the
// context error early, before any request is sent.
func TestGetCode_ContextCancelled(t *testing.T) {
	codeBytes := []byte{0x60, 0x00, 0x60, 0x00}
	codeHash := crypto.Keccak256Hash(codeBytes)

	// A valid response is available; the cancelled context must short-circuit
	// before the handler is ever reached.
	h := synctest.NewScriptedHandler([][]byte{synctest.MustMarshal(t, &syncpb.GetCodeResponse{Data: [][]byte{codeBytes}})})
	g := newTestGetter(t, h)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	codeSlices, err := g.GetCode(ctx, []common.Hash{codeHash})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, codeSlices)
	require.Zero(t, h.Calls())
}

// newTestGetter wires a getter whose client talks to a single in-memory peer
// serving h at the code-request handler ID.
func newTestGetter(t *testing.T, h p2p.Handler) *getter {
	t.Helper()
	net, tracker := synctest.NewLoopbackNetwork(t, p2p.EVMCodeRequestHandlerID, h)
	return &getter{
		client: NewClient(net, tracker),
		log:    loggingtest.New(t, logging.Debug),
	}
}
