// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
)

func TestGetProposedHeight(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)

	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Test through the exported NewHTTPHandler API
	handler, err := proVM.NewHTTPHandler(context.Background())
	require.NoError(err)
	require.NotNil(handler)

	// Create a test server with the handler
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create a Connect client to make requests to our handler
	client := proposervmconnect.NewProposerVMClient(
		http.DefaultClient,
		server.URL,
	)

	// Test the GetProposedHeight endpoint
	req := connect.NewRequest(&proposervm.GetProposedHeightRequest{})
	resp, err := client.GetProposedHeight(context.Background(), req)
	require.NoError(err)
	require.NotNil(resp.Msg)

	// Verify the response matches the expected minimum height
	expectedHeight, err := proVM.ctx.ValidatorState.GetMinimumHeight(context.Background())
	require.NoError(err)
	require.Equal(expectedHeight, resp.Msg.Height)
}
