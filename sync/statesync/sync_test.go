// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"runtime/pprof"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/subnet-evm/core/state/snapshot"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	statesyncclient "github.com/ava-labs/subnet-evm/sync/client"
	"github.com/ava-labs/subnet-evm/sync/handlers"
	handlerstats "github.com/ava-labs/subnet-evm/sync/handlers/stats"
	"github.com/ava-labs/subnet-evm/sync/statesync/statesynctest"
	"github.com/stretchr/testify/require"
)

const testSyncTimeout = 30 * time.Second

var errInterrupted = errors.New("interrupted sync")

type syncTest struct {
	ctx               context.Context
	prepareForTest    func(t *testing.T) (clientDB ethdb.Database, serverDB ethdb.Database, serverTrieDB *triedb.Database, syncRoot common.Hash)
	expectedError     error
	GetLeafsIntercept func(message.LeafsRequest, message.LeafsResponse) (message.LeafsResponse, error)
	GetCodeIntercept  func([]common.Hash, [][]byte) ([][]byte, error)
}

func testSync(t *testing.T, test syncTest) {
	t.Helper()
	ctx := context.Background()
	if test.ctx != nil {
		ctx = test.ctx
	}
	clientDB, serverDB, serverTrieDB, root := test.prepareForTest(t)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, nil, message.Codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverDB, message.Codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewMockClient(message.Codec, leafsRequestHandler, codeRequestHandler, nil)
	// Set intercept functions for the mock client
	mockClient.GetLeafsIntercept = test.GetLeafsIntercept
	mockClient.GetCodeIntercept = test.GetCodeIntercept

	s, err := NewStateSyncer(&StateSyncerConfig{
		Client:                   mockClient,
		Root:                     root,
		DB:                       clientDB,
		BatchSize:                1000, // Use a lower batch size in order to get test coverage of batches being written early.
		NumCodeFetchingWorkers:   DefaultNumCodeFetchingWorkers,
		MaxOutstandingCodeHashes: DefaultMaxOutstandingCodeHashes,
		RequestSize:              1024,
	})
	require.NoError(t, err, "failed to create state syncer")
	// begin sync
	s.Start(ctx)
	waitFor(t, context.Background(), s.Wait, test.expectedError, testSyncTimeout)
	if test.expectedError != nil {
		return
	}

	statesynctest.AssertDBConsistency(t, root, clientDB, serverTrieDB, triedb.NewDatabase(clientDB, nil))
}

// testSyncResumes tests a series of syncTests work as expected, invoking a callback function after each
// successive step.
func testSyncResumes(t *testing.T, steps []syncTest, stepCallback func()) {
	for _, test := range steps {
		testSync(t, test)
		stepCallback()
	}
}

// waitFor waits for a result on the [result] channel to match [expected], or a timeout.
func waitFor(t *testing.T, ctx context.Context, resultFunc func(context.Context) error, expected error, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := resultFunc(ctx)
	if ctx.Err() != nil {
		// print a stack trace to assist with debugging
		var stackBuf bytes.Buffer
		pprof.Lookup("goroutine").WriteTo(&stackBuf, 2)
		t.Log(stackBuf.String())
		// fail the test
		t.Fatal("unexpected timeout waiting for sync result")
	}

	require.ErrorIs(t, err, expected, "result of sync did not match expected error")
}

func TestSimpleSyncCases(t *testing.T) {
	var (
		numAccounts      = 250
		numAccountsSmall = 10
		clientErr        = errors.New("dummy client error")
	)
	tests := map[string]syncTest{
		"accounts": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, numAccounts, nil)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"accounts with code": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, numAccounts, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
					if index%3 == 0 {
						codeBytes := make([]byte, 256)
						_, err := rand.Read(codeBytes)
						require.NoError(t, err, "error reading random code bytes")

						codeHash := crypto.Keccak256Hash(codeBytes)
						rawdb.WriteCode(serverDB, codeHash, codeBytes)
						account.CodeHash = codeHash[:]
					}
					return account
				})
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"accounts with code and storage": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root := statesynctest.FillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, numAccounts)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"accounts with storage": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, numAccounts, func(t *testing.T, i int, account types.StateAccount) types.StateAccount {
					if i%5 == 0 {
						account.Root, _, _ = statesynctest.GenerateTrie(t, serverTrieDB, 16, common.HashLength)
					}

					return account
				})
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"accounts with overlapping storage": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := statesynctest.FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, numAccounts, 3)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"failed to fetch leafs": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, numAccountsSmall, nil)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
			GetLeafsIntercept: func(_ message.LeafsRequest, _ message.LeafsResponse) (message.LeafsResponse, error) {
				return message.LeafsResponse{}, clientErr
			},
			expectedError: clientErr,
		},
		"failed to fetch code": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root := statesynctest.FillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, numAccountsSmall)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
			GetCodeIntercept: func(_ []common.Hash, _ [][]byte) ([][]byte, error) {
				return nil, clientErr
			},
			expectedError: clientErr,
		},
	}
	for name, test := range tests {
		rand.New(rand.NewSource(1))
		t.Run(name, func(t *testing.T) {
			testSync(t, test)
		})
	}
}

func TestCancelSync(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)
	// Create trie with 2000 accounts (more than one leaf request)
	root := statesynctest.FillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, 2000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	testSync(t, syncTest{
		ctx: ctx,
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
		},
		expectedError: context.Canceled,
		GetLeafsIntercept: func(_ message.LeafsRequest, lr message.LeafsResponse) (message.LeafsResponse, error) {
			cancel()
			return lr, nil
		},
	})
}

// interruptLeafsIntercept provides the parameters to the getLeafsIntercept
// function which returns [errInterrupted] after passing through [numRequests]
// leafs requests for [root].
type interruptLeafsIntercept struct {
	numRequests    uint32
	interruptAfter uint32
	root           common.Hash
}

// getLeafsIntercept can be passed to mockClient and returns an unmodified
// response for the first [numRequest] requests for leafs from [root].
// After that, all requests for leafs from [root] return [errInterrupted].
func (i *interruptLeafsIntercept) getLeafsIntercept(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
	if request.Root == i.root {
		if numRequests := atomic.AddUint32(&i.numRequests, 1); numRequests > i.interruptAfter {
			return message.LeafsResponse{}, errInterrupted
		}
	}
	return response, nil
}

func TestResumeSyncAccountsTrieInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)
	root, _ := statesynctest.FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 2000, 3)
	clientDB := rawdb.NewMemoryDatabase()
	intercept := &interruptLeafsIntercept{
		root:           root,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	require.EqualValues(t, 2, intercept.numRequests)

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResumeSyncLargeStorageTrieInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := rawdb.NewMemoryDatabase()
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResumeSyncToNewRootAfterLargeStorageTrieInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot1, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	largeStorageRoot2, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root1, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot1
		}
		return account
	})
	root2, _ := statesynctest.FillAccounts(t, serverTrieDB, root1, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		if index == 20 {
			account.Root = largeStorageRoot2
		}
		return account
	})
	clientDB := rawdb.NewMemoryDatabase()
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot1,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root1
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	<-snapshot.WipeSnapshot(clientDB, false)

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root2
		},
	})
}

func TestResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for 2 successive accounts
		if index == 10 || index == 11 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := rawdb.NewMemoryDatabase()
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		if index == 10 || index == 90 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := rawdb.NewMemoryDatabase()
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResyncNewRootAfterDeletes(t *testing.T) {
	for name, test := range map[string]struct {
		deleteBetweenSyncs func(*testing.T, common.Hash, ethdb.Database)
	}{
		"delete code": {
			deleteBetweenSyncs: func(t *testing.T, _ common.Hash, clientDB ethdb.Database) {
				// delete code
				it := clientDB.NewIterator(rawdb.CodePrefix, nil)
				defer it.Release()
				for it.Next() {
					if len(it.Key()) != len(rawdb.CodePrefix)+common.HashLength {
						continue
					}
					require.NoError(t, clientDB.Delete(it.Key()), "failed to delete code hash %x", it.Key()[len(rawdb.CodePrefix):])
				}
				require.NoError(t, it.Error(), "error iterating over code hashes")
			},
		},
		"delete intermediate storage nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB ethdb.Database) {
				clientTrieDB := triedb.NewDatabase(clientDB, nil)
				tr, err := trie.New(trie.TrieID(root), clientTrieDB)
				require.NoError(t, err, "failed to create trie for root %s", root)
				nodeIt, err := tr.NodeIterator(nil)
				require.NoError(t, err, "failed to create node iterator for root %s", root)
				it := trie.NewIterator(nodeIt)
				accountsWithStorage := 0

				// keep track of storage tries we delete trie nodes from
				// so we don't try to do it again if another account has
				// the same storage root.
				corruptedStorageRoots := make(map[common.Hash]struct{})
				for it.Next() {
					var acc types.StateAccount
					require.NoError(t, rlp.DecodeBytes(it.Value, &acc), "failed to decode account at key %x", it.Key)
					if acc.Root == types.EmptyRootHash {
						continue
					}
					if _, found := corruptedStorageRoots[acc.Root]; found {
						// avoid trying to delete nodes from a trie we have already deleted nodes from
						continue
					}
					accountsWithStorage++
					if accountsWithStorage%2 != 0 {
						continue
					}
					corruptedStorageRoots[acc.Root] = struct{}{}
					tr, err := trie.New(trie.TrieID(acc.Root), clientTrieDB)
					require.NoError(t, err, "failed to create trie for root %s", acc.Root)
					statesynctest.CorruptTrie(t, clientDB, tr, 2)
				}
				require.NoError(t, it.Err, "error iterating over trie nodes")
			},
		},
		"delete intermediate account trie nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB ethdb.Database) {
				clientTrieDB := triedb.NewDatabase(clientDB, nil)
				tr, err := trie.New(trie.TrieID(root), clientTrieDB)
				require.NoError(t, err, "failed to create trie for root %s", root)
				statesynctest.CorruptTrie(t, clientDB, tr, 5)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			testSyncerSyncsToNewRoot(t, test.deleteBetweenSyncs)
		})
	}
}

func testSyncerSyncsToNewRoot(t *testing.T, deleteBetweenSyncs func(*testing.T, common.Hash, ethdb.Database)) {
	rand.New(rand.NewSource(1))
	clientDB := rawdb.NewMemoryDatabase()
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	root1, _ := statesynctest.FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 1000, 3)
	root2, _ := statesynctest.FillAccountsWithOverlappingStorage(t, serverTrieDB, root1, 1000, 3)

	called := false

	testSyncResumes(t, []syncTest{
		{
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				return clientDB, serverDB, serverTrieDB, root1
			},
		},
		{
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				return clientDB, serverDB, serverTrieDB, root2
			},
		},
	}, func() {
		// Only perform the delete stage once
		if called {
			return
		}
		called = true
		// delete snapshot first since this is not the responsibility of the EVM State Syncer
		<-snapshot.WipeSnapshot(clientDB, false)

		deleteBetweenSyncs(t, root1, clientDB)
	})
}

func TestDifferentWaitContext(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)
	// Create trie with many accounts to ensure sync takes time
	root := statesynctest.FillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, 2000)
	clientDB := rawdb.NewMemoryDatabase()

	// Track requests to show sync continues after Wait returns
	var requestCount int64

	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, nil, message.Codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverDB, message.Codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewMockClient(message.Codec, leafsRequestHandler, codeRequestHandler, nil)

	// Intercept to track ongoing requests and add delay
	mockClient.GetLeafsIntercept = func(req message.LeafsRequest, resp message.LeafsResponse) (message.LeafsResponse, error) {
		atomic.AddInt64(&requestCount, 1)
		// Add small delay to ensure sync is ongoing
		time.Sleep(10 * time.Millisecond)
		return resp, nil
	}

	s, err := NewStateSyncer(&StateSyncerConfig{
		Client:                   mockClient,
		Root:                     root,
		DB:                       clientDB,
		BatchSize:                1000,
		NumCodeFetchingWorkers:   DefaultNumCodeFetchingWorkers,
		MaxOutstandingCodeHashes: DefaultMaxOutstandingCodeHashes,
		RequestSize:              1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create two different contexts
	startCtx := context.Background() // Never cancelled
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer waitCancel()

	// Start with one context
	require.NoError(t, s.Start(startCtx), "failed to start state syncer")

	// Wait with different context that will timeout
	err = s.Wait(waitCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded, "Wait should return DeadlineExceeded error")

	// Check if more requests were made after Wait returned
	requestsWhenWaitReturned := atomic.LoadInt64(&requestCount)
	time.Sleep(100 * time.Millisecond)
	requestsAfterWait := atomic.LoadInt64(&requestCount)
	require.Equal(t, requestsWhenWaitReturned, requestsAfterWait, "Sync should not continue after Wait returned with different context")
}
