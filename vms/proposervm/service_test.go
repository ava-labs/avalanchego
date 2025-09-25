// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
)

func TestGetProposedHeight(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)

	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Set up core VM to build and accept blocks
	currentHeight := uint64(2000)
	minHeight := uint64(1000)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return currentHeight, nil
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return minHeight, nil
	}

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// Build and accept a block to initialize the VM state
	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(builtBlk.Accept(context.Background()))

	// Set up LastAccepted for the core VM
	coreVM.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF([]*snowmantest.Block{
		snowmantest.Genesis,
		coreBlk,
	})

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

	// Verify the response matches our expected P-Chain height
	require.Equal(minHeight, resp.Msg.Height)
}
