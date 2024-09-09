// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm/message"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/sync/syncutils"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
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
	if err != nil {
		t.Fatal(err)
	}
	// begin sync
	s.Start(ctx)
	waitFor(t, s.Done(), test.expectedError, testSyncTimeout)
	if test.expectedError != nil {
		return
	}

	assertDBConsistency(t, root, clientDB, serverTrieDB, triedb.NewDatabase(clientDB, nil))
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
func waitFor(t *testing.T, result <-chan error, expected error, timeout time.Duration) {
	t.Helper()
	select {
	case err := <-result:
		if expected != nil {
			if err == nil {
				t.Fatalf("Expected error %s, but got nil", expected)
			}
			assert.Contains(t, err.Error(), expected.Error())
		} else if err != nil {
			t.Fatal("unexpected error waiting for sync result", err)
		}
	case <-time.After(timeout):
		// print a stack trace to assist with debugging
		// if the test times out.
		var stackBuf bytes.Buffer
		pprof.Lookup("goroutine").WriteTo(&stackBuf, 2)
		t.Log(stackBuf.String())
		// fail the test
		t.Fatal("unexpected timeout waiting for sync result")
	}
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
				root, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, numAccounts, nil)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"accounts with code": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, numAccounts, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
					if index%3 == 0 {
						codeBytes := make([]byte, 256)
						_, err := rand.Read(codeBytes)
						if err != nil {
							t.Fatalf("error reading random code bytes: %v", err)
						}

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
				root := fillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, numAccounts)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"accounts with storage": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, numAccounts, func(t *testing.T, i int, account types.StateAccount) types.StateAccount {
					if i%5 == 0 {
						account.Root, _, _ = syncutils.GenerateTrie(t, serverTrieDB, 16, common.HashLength)
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
				root, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, numAccounts, 3)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
		},
		"failed to fetch leafs": {
			prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				serverDB := rawdb.NewMemoryDatabase()
				serverTrieDB := triedb.NewDatabase(serverDB, nil)
				root, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, numAccountsSmall, nil)
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
				root := fillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, numAccountsSmall)
				return rawdb.NewMemoryDatabase(), serverDB, serverTrieDB, root
			},
			GetCodeIntercept: func(_ []common.Hash, _ [][]byte) ([][]byte, error) {
				return nil, clientErr
			},
			expectedError: clientErr,
		},
	}
	for name, test := range tests {
		rand.Seed(1)
		t.Run(name, func(t *testing.T) {
			testSync(t, test)
		})
	}
}

func TestCancelSync(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)
	// Create trie with 2000 accounts (more than one leaf request)
	root := fillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, 2000)
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
	root, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 2000, 3)
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

	assert.EqualValues(t, 2, intercept.numRequests)

	testSync(t, syncTest{
		prepareForTest: func(t *testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResumeSyncLargeStorageTrieInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot, _, _ := syncutils.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
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

	largeStorageRoot1, _, _ := syncutils.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	largeStorageRoot2, _, _ := syncutils.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root1, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot1
		}
		return account
	})
	root2, _ := syncutils.FillAccounts(t, serverTrieDB, root1, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
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

	largeStorageRoot, _, _ := syncutils.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
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

	largeStorageRoot, _, _ := syncutils.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := syncutils.FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
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
					if err := clientDB.Delete(it.Key()); err != nil {
						t.Fatal(err)
					}
				}
				if err := it.Error(); err != nil {
					t.Fatal(err)
				}
			},
		},
		"delete intermediate storage nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB ethdb.Database) {
				clientTrieDB := triedb.NewDatabase(clientDB, nil)
				tr, err := trie.New(trie.TrieID(root), clientTrieDB)
				if err != nil {
					t.Fatal(err)
				}
				nodeIt, err := tr.NodeIterator(nil)
				if err != nil {
					t.Fatal(err)
				}
				it := trie.NewIterator(nodeIt)
				accountsWithStorage := 0

				// keep track of storage tries we delete trie nodes from
				// so we don't try to do it again if another account has
				// the same storage root.
				corruptedStorageRoots := make(map[common.Hash]struct{})
				for it.Next() {
					var acc types.StateAccount
					if err := rlp.DecodeBytes(it.Value, &acc); err != nil {
						t.Fatal(err)
					}
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
					if err != nil {
						t.Fatal(err)
					}
					syncutils.CorruptTrie(t, clientDB, tr, 2)
				}
				if err := it.Err; err != nil {
					t.Fatal(err)
				}
			},
		},
		"delete intermediate account trie nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB ethdb.Database) {
				clientTrieDB := triedb.NewDatabase(clientDB, nil)
				tr, err := trie.New(trie.TrieID(root), clientTrieDB)
				if err != nil {
					t.Fatal(err)
				}
				syncutils.CorruptTrie(t, clientDB, tr, 5)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			testSyncerSyncsToNewRoot(t, test.deleteBetweenSyncs)
		})
	}
}

func testSyncerSyncsToNewRoot(t *testing.T, deleteBetweenSyncs func(*testing.T, common.Hash, ethdb.Database)) {
	rand.Seed(1)
	clientDB := rawdb.NewMemoryDatabase()
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	root1, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 1000, 3)
	root2, _ := FillAccountsWithOverlappingStorage(t, serverTrieDB, root1, 1000, 3)

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
