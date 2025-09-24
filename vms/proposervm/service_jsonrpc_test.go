// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	avajson "github.com/ava-labs/avalanchego/utils/json"
)

func TestServiceJsonRPCGetProposedHeight(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)

	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Create JSON-RPC server
	server := rpc.NewServer()
	server.RegisterCodec(avajson.NewCodec(), "application/json")
	proposerAPI := &ProposerAPI{vm: proVM}
	err := server.RegisterService(proposerAPI, "proposervm")
	require.NoError(err)

	// Test the GetProposedHeight method directly
	var reply api.GetHeightResponse
	err = proposerAPI.GetProposedHeight(&http.Request{}, &struct{}{}, &reply)
	require.NoError(err)

	// Verify the response matches the expected minimum height
	expectedHeight, err := proVM.ctx.ValidatorState.GetMinimumHeight(context.Background())
	require.NoError(err)
	require.Equal(avajson.Uint64(expectedHeight), reply.Height)
}
