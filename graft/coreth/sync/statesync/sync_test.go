// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers"
	"github.com/ava-labs/coreth/sync/statesync/statesynctest"

	statesyncclient "github.com/ava-labs/coreth/sync/client"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
)

const testRequestSize = 1024

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
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverTrieDB, message.StateTrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverDB, message.Codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewTestClient(message.Codec, leafsRequestHandler, codeRequestHandler, nil)
	// Set intercept functions for the mock client
	mockClient.GetLeafsIntercept = test.GetLeafsIntercept
	mockClient.GetCodeIntercept = test.GetCodeIntercept

	// Create the code fetcher.
	fetcher, err := NewCodeQueue(clientDB, make(chan struct{}))
	require.NoError(t, err, "failed to create code fetcher")

	// Create the consumer code syncer.
	codeSyncer, err := NewCodeSyncer(mockClient, clientDB, fetcher.CodeHashes())
	require.NoError(t, err, "failed to create code syncer")

	cfg := Config{
		BatchSize:   1000, // Use a lower batch size in order to get test coverage of batches being written early.
		RequestSize: testRequestSize,
	}

	// Create the state syncer.
	stateSyncer, err := NewSyncer(mockClient, clientDB, root, fetcher, cfg)
	require.NoError(t, err, "failed to create state syncer")

	// Run both syncers concurrently and wait for the first error.
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })

	err = eg.Wait()
	require.ErrorIs(t, err, test.expectedError, "unexpected error during sync")

	// Only assert database consistency if the sync was expected to succeed.
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
				root := fillAccountsWithStorage(t, serverDB, serverTrieDB, common.Hash{}, numAccounts)
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
	t.Cleanup(cancel)

	testSync(t, syncTest{
		ctx: ctx,
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
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

// getLeafsIntercept can be passed to testClient and returns an unmodified
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
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	require.Equal(t, uint32(2), intercept.numRequests)

	testSync(t, syncTest{
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResumeSyncLargeStorageTrieInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(_ *testing.T, index int, account types.StateAccount) types.StateAccount {
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
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResumeSyncToNewRootAfterLargeStorageTrieInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot1, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	largeStorageRoot2, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root1, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 2000, func(_ *testing.T, index int, account types.StateAccount) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot1
		}
		return account
	})
	root2, _ := statesynctest.FillAccounts(t, serverTrieDB, root1, 100, func(_ *testing.T, index int, account types.StateAccount) types.StateAccount {
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
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root1
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	<-snapshot.WipeSnapshot(clientDB, false)

	testSync(t, syncTest{
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root2
		},
	})
}

func TestResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(_ *testing.T, index int, account types.StateAccount) types.StateAccount {
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
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
	})
}

func TestResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterrupted(t *testing.T) {
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	largeStorageRoot, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, 2000, common.HashLength)
	root, _ := statesynctest.FillAccounts(t, serverTrieDB, common.Hash{}, 100, func(_ *testing.T, index int, account types.StateAccount) types.StateAccount {
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
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
			return clientDB, serverDB, serverTrieDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
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
	rand.Seed(1)
	clientDB := rawdb.NewMemoryDatabase()
	serverDB := rawdb.NewMemoryDatabase()
	serverTrieDB := triedb.NewDatabase(serverDB, nil)

	root1, _ := statesynctest.FillAccountsWithOverlappingStorage(t, serverTrieDB, common.Hash{}, 1000, 3)
	root2, _ := statesynctest.FillAccountsWithOverlappingStorage(t, serverTrieDB, root1, 1000, 3)

	called := false

	testSyncResumes(t, []syncTest{
		{
			prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
				return clientDB, serverDB, serverTrieDB, root1
			},
		},
		{
			prepareForTest: func(*testing.T) (ethdb.Database, ethdb.Database, *triedb.Database, common.Hash) {
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

// assertDBConsistency checks [serverTrieDB] and [clientTrieDB] have the same EVM state trie at [root],
// and that [clientTrieDB.DiskDB] has corresponding account & snapshot values.
// Also verifies any code referenced by the EVM state is present in [clientTrieDB] and the hash is correct.
func assertDBConsistency(t testing.TB, root common.Hash, clientDB ethdb.Database, serverTrieDB, clientTrieDB *triedb.Database) {
	numSnapshotAccounts := 0
	accountIt := customrawdb.IterateAccountSnapshots(clientDB)
	defer accountIt.Release()
	for accountIt.Next() {
		if !bytes.HasPrefix(accountIt.Key(), rawdb.SnapshotAccountPrefix) || len(accountIt.Key()) != len(rawdb.SnapshotAccountPrefix)+common.HashLength {
			continue
		}
		numSnapshotAccounts++
	}
	require.NoError(t, accountIt.Error(), "error iterating over account snapshots")
	trieAccountLeaves := 0

	statesynctest.AssertTrieConsistency(t, root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
		trieAccountLeaves++
		accHash := common.BytesToHash(key)
		var acc types.StateAccount
		if err := rlp.DecodeBytes(val, &acc); err != nil {
			return err
		}
		// check snapshot consistency
		snapshotVal := rawdb.ReadAccountSnapshot(clientDB, accHash)
		expectedSnapshotVal := types.SlimAccountRLP(acc)
		require.Equal(t, expectedSnapshotVal, snapshotVal)

		// check code consistency
		if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash[:]) {
			codeHash := common.BytesToHash(acc.CodeHash)
			code := rawdb.ReadCode(clientDB, codeHash)
			actualHash := crypto.Keccak256Hash(code)
			require.NotEmpty(t, code)
			require.Equal(t, codeHash, actualHash)
		}
		if acc.Root == types.EmptyRootHash {
			return nil
		}

		storageIt := rawdb.IterateStorageSnapshots(clientDB, accHash)
		defer storageIt.Release()

		snapshotStorageKeysCount := 0
		for storageIt.Next() {
			snapshotStorageKeysCount++
		}

		storageTrieLeavesCount := 0

		// check storage trie and storage snapshot consistency
		statesynctest.AssertTrieConsistency(t, acc.Root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
			storageTrieLeavesCount++
			snapshotVal := rawdb.ReadStorageSnapshot(clientDB, accHash, common.BytesToHash(key))
			require.Equal(t, val, snapshotVal)
			return nil
		})

		require.Equal(t, storageTrieLeavesCount, snapshotStorageKeysCount)
		return nil
	})

	// Check that the number of accounts in the snapshot matches the number of leaves in the accounts trie
	require.Equal(t, trieAccountLeaves, numSnapshotAccounts)
}

func fillAccountsWithStorage(t *testing.T, serverDB ethdb.Database, serverTrieDB *triedb.Database, root common.Hash, numAccounts int) common.Hash {
	newRoot, _ := statesynctest.FillAccounts(t, serverTrieDB, root, numAccounts, func(_ *testing.T, _ int, account types.StateAccount) types.StateAccount {
		codeBytes := make([]byte, 256)
		_, err := rand.Read(codeBytes)
		require.NoError(t, err, "error reading random code bytes")

		codeHash := crypto.Keccak256Hash(codeBytes)
		rawdb.WriteCode(serverDB, codeHash, codeBytes)
		account.CodeHash = codeHash[:]

		// now create state trie
		numKeys := 16
		account.Root, _, _ = statesynctest.GenerateTrie(t, serverTrieDB, numKeys, common.HashLength)
		return account
	})
	return newRoot
}
