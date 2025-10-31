// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers"
	"github.com/ava-labs/coreth/utils/utilstest"

	statesyncclient "github.com/ava-labs/coreth/sync/client"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
)

type codeSyncerTest struct {
	clientDB          ethdb.Database
	queueCapacity     int
	codeRequestHashes [][]common.Hash
	codeByteSlices    [][]byte
	getCodeIntercept  func(hashes []common.Hash, codeBytes [][]byte) ([][]byte, error)
	err               error
}

func testCodeSyncer(t *testing.T, test codeSyncerTest) {
	// Set up serverDB
	serverDB := memorydb.New()

	codeHashes := make([]common.Hash, 0, len(test.codeByteSlices))
	for _, codeBytes := range test.codeByteSlices {
		codeHash := crypto.Keccak256Hash(codeBytes)
		rawdb.WriteCode(serverDB, codeHash, codeBytes)
		codeHashes = append(codeHashes, codeHash)
	}

	// Set up mockClient
	codeRequestHandler := handlers.NewCodeRequestHandler(serverDB, message.Codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewTestClient(message.Codec, nil, codeRequestHandler, nil)
	mockClient.GetCodeIntercept = test.getCodeIntercept

	clientDB := test.clientDB
	if clientDB == nil {
		clientDB = rawdb.NewMemoryDatabase()
	}

	codeQueue, err := NewCodeQueue(
		clientDB,
		make(chan struct{}),
		WithCapacity(test.queueCapacity),
	)
	require.NoError(t, err)

	codeSyncer, err := NewCodeSyncer(
		mockClient,
		clientDB,
		codeQueue.CodeHashes(),
	)
	require.NoError(t, err)
	go func() {
		for _, codeHashes := range test.codeRequestHashes {
			if err := codeQueue.AddCode(codeHashes); err != nil {
				require.ErrorIs(t, err, test.err)
			}
		}
		if err := codeQueue.Finalize(); err != nil {
			require.ErrorIs(t, err, test.err)
		}
	}()

	ctx, cancel := utilstest.NewTestContext(t)
	t.Cleanup(cancel)

	// Run the sync and handle expected error.
	err = codeSyncer.Sync(ctx)
	require.ErrorIs(t, err, test.err)
	if err != nil {
		return // don't check the state
	}

	// Assert that the client synced the code correctly.
	for i, codeHash := range codeHashes {
		codeBytes := rawdb.ReadCode(clientDB, codeHash)
		require.Equal(t, test.codeByteSlices[i], codeBytes)
	}
}

func TestCodeSyncerSingleCodeHash(t *testing.T) {
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	testCodeSyncer(t, codeSyncerTest{
		codeRequestHashes: [][]common.Hash{{codeHash}},
		codeByteSlices:    [][]byte{codeBytes},
	})
}

func TestCodeSyncerManyCodeHashes(t *testing.T) {
	numCodeSlices := 5000
	codeHashes := make([]common.Hash, 0, numCodeSlices)
	codeByteSlices := make([][]byte, 0, numCodeSlices)
	for i := 0; i < numCodeSlices; i++ {
		codeBytes := utils.RandomBytes(100)
		codeHash := crypto.Keccak256Hash(codeBytes)
		codeHashes = append(codeHashes, codeHash)
		codeByteSlices = append(codeByteSlices, codeBytes)
	}

	testCodeSyncer(t, codeSyncerTest{
		queueCapacity:     10,
		codeRequestHashes: [][]common.Hash{codeHashes[0:100], codeHashes[100:2000], codeHashes[2000:2005], codeHashes[2005:]},
		codeByteSlices:    codeByteSlices,
	})
}

func TestCodeSyncerRequestErrors(t *testing.T) {
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	err := errors.New("dummy error")
	testCodeSyncer(t, codeSyncerTest{
		codeRequestHashes: [][]common.Hash{{codeHash}},
		codeByteSlices:    [][]byte{codeBytes},
		getCodeIntercept: func([]common.Hash, [][]byte) ([][]byte, error) {
			return nil, err
		},
		err: err,
	})
}

func TestCodeSyncerAddsInProgressCodeHashes(t *testing.T) {
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	clientDB := rawdb.NewMemoryDatabase()
	customrawdb.AddCodeToFetch(clientDB, codeHash)
	testCodeSyncer(t, codeSyncerTest{
		clientDB:          clientDB,
		codeRequestHashes: nil,
		codeByteSlices:    [][]byte{codeBytes},
	})
}

func TestCodeSyncerAddsMoreInProgressThanQueueSize(t *testing.T) {
	numCodeSlices := 100
	codeHashes := make([]common.Hash, 0, numCodeSlices)
	codeByteSlices := make([][]byte, 0, numCodeSlices)
	for i := 0; i < numCodeSlices; i++ {
		codeBytes := utils.RandomBytes(100)
		codeHash := crypto.Keccak256Hash(codeBytes)
		codeHashes = append(codeHashes, codeHash)
		codeByteSlices = append(codeByteSlices, codeBytes)
	}

	db := rawdb.NewMemoryDatabase()
	for _, codeHash := range codeHashes {
		customrawdb.AddCodeToFetch(db, codeHash)
	}

	testCodeSyncer(t, codeSyncerTest{
		clientDB:          db,
		codeRequestHashes: nil,
		codeByteSlices:    codeByteSlices,
	})
}
