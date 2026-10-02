// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"
)

// TestCallTimeout tests that the RPC call timeout ends calls that never return
// on their own, over both HTTP and WebSockets.
func TestCallTimeout(t *testing.T) {
	blocking := common.Address{'b', 'l', 'o', 'c', 'k'}
	precompileOpt, unblock := withBlockingPrecompile(blocking)
	ctx, sut := newSUT(t, 0, precompileOpt, options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.RPCConfig.CallTimeout = 100 * time.Millisecond
	}))
	defer unblock()

	handlers, err := sut.CreateHandlers(ctx)
	require.NoErrorf(t, err, "%T.CreateHandlers()", sut.ChainVM)
	server := httptest.NewServer(handlers[rpcHTTPExtensionPath])
	t.Cleanup(server.Close)
	httpClient, err := rpc.DialContext(ctx, server.URL)
	require.NoErrorf(t, err, "rpc.DialContext(%q)", server.URL)
	t.Cleanup(httpClient.Close)

	tests := []struct {
		name   string
		client *rpc.Client
	}{
		{
			name:   "http",
			client: httpClient,
		},
		{
			name:   "ws",
			client: sut.rpcClient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The precompile ignores cancellation, so only the call timeout
			// can end the call.
			err := tt.client.CallContext(ctx, nil, "eth_call", map[string]any{"to": blocking}, "latest")
			var rpcErr rpc.Error
			require.ErrorAsf(t, err, &rpcErr, "%T.CallContext(eth_call)", tt.client)
			require.Equal(t, rpc.ErrCodeTimeout, rpcErr.ErrorCode(), "eth_call() error code")
		})
	}
}
