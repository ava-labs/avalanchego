// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

func TestServiceJsonRPCGetProposedHeight(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)

	// Create the ProposerVM and set up the state properly
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

	// Create JSON-RPC server
	server := rpc.NewServer()
	server.RegisterCodec(avajson.NewCodec(), "application/json")
	proposerAPI := &ProposerAPI{vm: proVM}
	require.NoError(server.RegisterService(proposerAPI, "proposervm"))

	// Test the GetProposedHeight method directly
	var reply api.GetHeightResponse
	require.NoError(proposerAPI.GetProposedHeight(&http.Request{}, &struct{}{}, &reply))

	// Verify the response matches our expected P-Chain height
	require.Equal(avajson.Uint64(minHeight), reply.Height)
}
