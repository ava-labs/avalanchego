// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/connectclient"
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

func TestConnectRPCService_GetProposedHeight(t *testing.T) {
	require := require.New(t)

	const pChainHeight = 123
	_, _, vm, _ := initTestProposerVM(t, upgradetest.Durango, upgrade.InitiallyActiveTime, pChainHeight)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	// Test through the exported NewHTTPHandler API
	handler, err := vm.NewHTTPHandler(context.Background())
	require.NoError(err)
	require.NotNil(handler)

	// Create a test server with the handler
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create a Connect client to make requests to our handler
	client := proposervmconnect.NewProposerVMClient(
		http.DefaultClient,
		server.URL,
		connect.WithInterceptors(
			connectclient.SetRouteHeaderInterceptor{Route: []string{"dummy-chain-id", HTTPHeaderRoute}},
		),
	)

	// Test the GetProposedHeight endpoint
	req := connect.NewRequest(&proposervm.GetProposedHeightRequest{})
	resp, err := client.GetProposedHeight(context.Background(), req)
	require.NoError(err)
	require.NotNil(resp)
	require.NotNil(resp.Msg)

	// Verify the response matches our expected P-Chain height
	require.Equal(uint64(pChainHeight), resp.Msg.GetHeight())
}

func TestJSONRPCService_GetProposedHeight(t *testing.T) {
	require := require.New(t)

	const pChainHeight = 123
	_, _, vm, _ := initTestProposerVM(t, upgradetest.Durango, upgrade.InitiallyActiveTime, pChainHeight)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	s := &jsonrpcService{vm: vm}
	var reply api.GetHeightResponse
	require.NoError(s.GetProposedHeight(&http.Request{URL: &url.URL{}}, &struct{}{}, &reply))
	require.Equal(api.GetHeightResponse{Height: pChainHeight}, reply)
}
