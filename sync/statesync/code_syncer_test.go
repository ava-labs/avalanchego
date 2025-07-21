// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	statesyncclient "github.com/ava-labs/subnet-evm/sync/client"
	"github.com/ava-labs/subnet-evm/sync/handlers"
	handlerstats "github.com/ava-labs/subnet-evm/sync/handlers/stats"
	"github.com/stretchr/testify/assert"
)

type codeSyncerTest struct {
	setupCodeSyncer   func(*codeSyncer)
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
	mockClient := statesyncclient.NewMockClient(message.Codec, nil, codeRequestHandler, nil)
	mockClient.GetCodeIntercept = test.getCodeIntercept

	clientDB := rawdb.NewMemoryDatabase()

	codeSyncer := newCodeSyncer(CodeSyncerConfig{
		MaxOutstandingCodeHashes: DefaultMaxOutstandingCodeHashes,
		NumCodeFetchingWorkers:   DefaultNumCodeFetchingWorkers,
		Client:                   mockClient,
		DB:                       clientDB,
	})
	if test.setupCodeSyncer != nil {
		test.setupCodeSyncer(codeSyncer)
	}
	codeSyncer.start(context.Background())

	for _, codeHashes := range test.codeRequestHashes {
		if err := codeSyncer.addCode(codeHashes); err != nil {
			if test.err == nil {
				t.Fatal(err)
			} else {
				assert.ErrorIs(t, err, test.err)
			}
		}
	}
	codeSyncer.notifyAccountTrieCompleted()

	err := <-codeSyncer.Done()
	if test.err != nil {
		if err == nil {
			t.Fatal(t, "expected non-nil error: %s", test.err)
		}
		assert.ErrorIs(t, err, test.err)
		return
	} else if err != nil {
		t.Fatal(err)
	}

	// Assert that the client synced the code correctly.
	for i, codeHash := range codeHashes {
		codeBytes := rawdb.ReadCode(clientDB, codeHash)
		assert.Equal(t, test.codeByteSlices[i], codeBytes)
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
		setupCodeSyncer: func(c *codeSyncer) {
			c.codeHashes = make(chan common.Hash, 10)
		},
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
		getCodeIntercept: func(hashes []common.Hash, codeBytes [][]byte) ([][]byte, error) {
			return nil, err
		},
		err: err,
	})
}

func TestCodeSyncerAddsInProgressCodeHashes(t *testing.T) {
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	testCodeSyncer(t, codeSyncerTest{
		setupCodeSyncer: func(c *codeSyncer) {
			customrawdb.AddCodeToFetch(c.DB, codeHash)
		},
		codeRequestHashes: nil,
		codeByteSlices:    [][]byte{codeBytes},
	})
}

func TestCodeSyncerAddsMoreInProgressThanQueueSize(t *testing.T) {
	numCodeSlices := 10
	codeHashes := make([]common.Hash, 0, numCodeSlices)
	codeByteSlices := make([][]byte, 0, numCodeSlices)
	for i := 0; i < numCodeSlices; i++ {
		codeBytes := utils.RandomBytes(100)
		codeHash := crypto.Keccak256Hash(codeBytes)
		codeHashes = append(codeHashes, codeHash)
		codeByteSlices = append(codeByteSlices, codeBytes)
	}

	testCodeSyncer(t, codeSyncerTest{
		setupCodeSyncer: func(c *codeSyncer) {
			for _, codeHash := range codeHashes {
				customrawdb.AddCodeToFetch(c.DB, codeHash)
			}
			c.codeHashes = make(chan common.Hash, numCodeSlices/2)
		},
		codeRequestHashes: nil,
		codeByteSlices:    codeByteSlices,
	})
}
