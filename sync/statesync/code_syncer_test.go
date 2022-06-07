// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestCodeSyncerFetchCode(t *testing.T) {
	// Set up serverDB
	serverDB := memorydb.New()
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(serverDB, codeHash, codeBytes)

	// Set up mockClient
	codeRequestHandler := handlers.NewCodeRequestHandler(serverDB, message.Codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewMockClient(message.Codec, nil, codeRequestHandler, nil)

	clientDB := memorydb.New()

	codeSyncer := newCodeSyncer(clientDB, mockClient)

	// Start code syncer with the given context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	codeSyncer.start(ctx)

	if err := codeSyncer.addCode(codeHash); err != nil {
		t.Fatal(err)
	}

	garbageCodeHash := common.Hash{1}
	cancel()
	if err := codeSyncer.addCode(garbageCodeHash); err == nil {
		t.Fatal("Expected fetch code to fail to fetch garbage hash due to cancelled context")
	}
}
