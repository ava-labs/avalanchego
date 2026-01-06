// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	statesyncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
	handlerstats "github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats"
)

const testRequestSize = 1024

var errInterrupted = errors.New("interrupted sync")

type syncTest struct {
	ctx               context.Context
	prepareForTest    func(t *testing.T, r *rand.Rand) (clientDB state.Database, serverDB state.Database, syncRoot common.Hash)
	expectedError     error
	GetLeafsIntercept func(message.LeafsRequest, message.LeafsResponse) (message.LeafsResponse, error)
	GetCodeIntercept  func([]common.Hash, [][]byte) ([][]byte, error)
}

func testSync(t *testing.T, test syncTest) {
	t.Helper()
	ctx := t.Context()
	if test.ctx != nil {
		ctx = test.ctx
	}
	r := rand.New(rand.NewSource(1))
	clientDB, serverDB, root := test.prepareForTest(t, r)
	clientEthDB := clientDB.DiskDB().(ethdb.Database)
	serverEthDB := serverDB.DiskDB().(ethdb.Database)
	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverDB.TrieDB(), message.StateTrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverEthDB, message.Codec, handlerstats.NewNoopHandlerStats())
	mockClient := statesyncclient.NewTestClient(message.Codec, leafsRequestHandler, codeRequestHandler, nil)
	// Set intercept functions for the mock client
	mockClient.GetLeafsIntercept = test.GetLeafsIntercept
	mockClient.GetCodeIntercept = test.GetCodeIntercept

	// Create the code fetcher.
	fetcher, err := NewCodeQueue(clientEthDB, make(chan struct{}))
	require.NoError(t, err, "failed to create code fetcher")

	// Create the consumer code syncer.
	codeSyncer, err := NewCodeSyncer(mockClient, clientEthDB, fetcher.CodeHashes())
	require.NoError(t, err, "failed to create code syncer")

	// Create the state syncer.
	stateSyncer, err := NewSyncer(
		mockClient,
		clientEthDB,
		root,
		fetcher,
		testRequestSize,
		WithBatchSize(1000), // Use a lower batch size in order to get test coverage of batches being written early.
	)
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

	assertDBConsistency(t, root, clientDB, serverDB)
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
			prepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, numAccounts, nil)
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
		},
		"accounts with code": {
			prepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, numAccounts, func(t *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
					if index%3 == 0 {
						codeBytes := make([]byte, 256)
						_, err := r.Read(codeBytes)
						require.NoError(t, err, "error reading random code bytes")

						codeHash := crypto.Keccak256Hash(codeBytes)
						rawdb.WriteCode(serverDB.DiskDB(), codeHash, codeBytes)
						account.CodeHash = codeHash[:]
					}
					return account
				})
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
		},
		"accounts with code and storage": {
			prepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root := fillAccountsWithStorage(t, r, serverDB, common.Hash{}, numAccounts)
				return serverDB, serverDB, root
			},
		},
		"accounts with storage": {
			prepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, numAccounts, func(t *testing.T, i int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
					if i%5 == 0 {
						synctest.FillStorageForAccount(t, r, types.EmptyRootHash, 16, addr, storageTr)
					}
					return account
				})
				return serverDB, serverDB, root
			},
		},
		"accounts with overlapping storage": {
			prepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, numAccounts, 3)
				return serverDB, serverDB, root
			},
		},
		"failed to fetch leafs": {
			prepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, numAccountsSmall, nil)
				return serverDB, serverDB, root
			},
			GetLeafsIntercept: func(_ message.LeafsRequest, _ message.LeafsResponse) (message.LeafsResponse, error) {
				return message.LeafsResponse{}, clientErr
			},
			expectedError: clientErr,
		},
		"failed to fetch code": {
			prepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root := fillAccountsWithStorage(t, r, serverDB, common.Hash{}, numAccountsSmall)
				return serverDB, serverDB, root
			},
			GetCodeIntercept: func(_ []common.Hash, _ [][]byte) ([][]byte, error) {
				return nil, clientErr
			},
			expectedError: clientErr,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testSync(t, test)
		})
	}
}

func TestCancelSync(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	testSync(t, syncTest{
		ctx: ctx,
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			// Create trie with 2000 accounts (more than one leaf request)
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root := fillAccountsWithStorage(t, r, serverDB, common.Hash{}, 2000)
			return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
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
	numRequests    atomic.Uint32
	interruptAfter uint32
	root           common.Hash
}

// getLeafsIntercept can be passed to testClient and returns an unmodified
// response for the first [numRequest] requests for leafs from [root].
// After that, all requests for leafs from [root] return [errInterrupted].
func (i *interruptLeafsIntercept) getLeafsIntercept(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
	if request.Root == i.root {
		if numRequests := i.numRequests.Add(1); numRequests > i.interruptAfter {
			return message.LeafsResponse{}, errInterrupted
		}
	}
	return response, nil
}

func TestResumeSyncAccountsTrieInterrupted(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	root, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, 2000, 3)
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &interruptLeafsIntercept{
		root:           root,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	require.GreaterOrEqual(t, intercept.numRequests.Load(), uint32(2))

	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

func TestResumeSyncLargeStorageTrieInterrupted(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 2000, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

func TestResumeSyncToNewRootAfterLargeStorageTrieInterrupted(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot1, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	largeStorageRoot2, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root1, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 2000, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot1
		}
		return account
	})
	root2, _ := synctest.FillAccounts(t, r, serverDB, root1, 100, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		if index == 20 {
			account.Root = largeStorageRoot2
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot1,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root1
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	<-snapshot.WipeSnapshot(clientDB.DiskDB(), false)

	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root2
		},
	})
}

func TestResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterrupted(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 100, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		// Set the root for 2 successive accounts
		if index == 10 || index == 11 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

func TestResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterrupted(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 100, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		if index == 10 || index == 90 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &interruptLeafsIntercept{
		root:           largeStorageRoot,
		interruptAfter: 1,
	}
	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		expectedError:     errInterrupted,
		GetLeafsIntercept: intercept.getLeafsIntercept,
	})

	testSync(t, syncTest{
		prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

func TestResyncNewRootAfterDeletes(t *testing.T) {
	for name, test := range map[string]struct {
		deleteBetweenSyncs func(*testing.T, common.Hash, state.Database)
	}{
		"delete code": {
			deleteBetweenSyncs: func(t *testing.T, _ common.Hash, clientDB state.Database) {
				// delete code
				it := clientDB.DiskDB().NewIterator(rawdb.CodePrefix, nil)
				defer it.Release()
				for it.Next() {
					if len(it.Key()) != len(rawdb.CodePrefix)+common.HashLength {
						continue
					}
					require.NoError(t, clientDB.DiskDB().Delete(it.Key()), "failed to delete code hash %x", it.Key()[len(rawdb.CodePrefix):])
				}
				require.NoError(t, it.Error(), "error iterating over code hashes")
			},
		},
		"delete intermediate storage nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB state.Database) {
				clientTrieDB := clientDB.TrieDB()
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
					synctest.CorruptTrie(t, clientDB.DiskDB(), tr, 2)
				}
				require.NoError(t, it.Err, "error iterating over trie nodes")
			},
		},
		"delete intermediate account trie nodes": {
			deleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB state.Database) {
				clientTrieDB := clientDB.TrieDB()
				tr, err := trie.New(trie.TrieID(root), clientTrieDB)
				require.NoError(t, err, "failed to create trie for root %s", root)
				synctest.CorruptTrie(t, clientDB.DiskDB(), tr, 5)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testSyncerSyncsToNewRoot(t, test.deleteBetweenSyncs)
		})
	}
}

func testSyncerSyncsToNewRoot(t *testing.T, deleteBetweenSyncs func(*testing.T, common.Hash, state.Database)) {
	r := rand.New(rand.NewSource(1))
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	root1, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, 1000, 3)
	root2, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, root1, 1000, 3)

	called := false

	testSyncResumes(t, []syncTest{
		{
			prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
				return clientDB, serverDB, root1
			},
		},
		{
			prepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
				return clientDB, serverDB, root2
			},
		},
	}, func() {
		// Only perform the delete stage once
		if called {
			return
		}
		called = true
		// delete snapshot first since this is not the responsibility of the EVM State Syncer
		<-snapshot.WipeSnapshot(clientDB.DiskDB(), false)

		deleteBetweenSyncs(t, root1, clientDB)
	})
}

// assertDBConsistency checks [serverTrieDB] and [clientTrieDB] have the same EVM state trie at [root],
// and that [clientTrieDB.DiskDB] has corresponding account & snapshot values.
// Also verifies any code referenced by the EVM state is present in [clientTrieDB] and the hash is correct.
func assertDBConsistency(t testing.TB, root common.Hash, clientDB, serverDB state.Database) {
	numSnapshotAccounts := 0
	accountIt := customrawdb.NewAccountSnapshotsIterator(clientDB.DiskDB())
	defer accountIt.Release()
	for accountIt.Next() {
		if !bytes.HasPrefix(accountIt.Key(), rawdb.SnapshotAccountPrefix) || len(accountIt.Key()) != len(rawdb.SnapshotAccountPrefix)+common.HashLength {
			continue
		}
		numSnapshotAccounts++
	}
	require.NoError(t, accountIt.Error(), "error iterating over account snapshots")
	trieAccountLeaves := 0

	synctest.AssertTrieConsistency(t, root, serverDB.TrieDB(), clientDB.TrieDB(), func(key, val []byte) error {
		trieAccountLeaves++
		accHash := common.BytesToHash(key)
		var acc types.StateAccount
		if err := rlp.DecodeBytes(val, &acc); err != nil {
			return err
		}
		// check snapshot consistency
		snapshotVal := rawdb.ReadAccountSnapshot(clientDB.DiskDB(), accHash)
		expectedSnapshotVal := types.SlimAccountRLP(acc)
		require.Equal(t, expectedSnapshotVal, snapshotVal)

		// check code consistency
		if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash[:]) {
			codeHash := common.BytesToHash(acc.CodeHash)
			code := rawdb.ReadCode(clientDB.DiskDB(), codeHash)
			actualHash := crypto.Keccak256Hash(code)
			require.NotEmpty(t, code)
			require.Equal(t, codeHash, actualHash)
		}
		if acc.Root == types.EmptyRootHash {
			return nil
		}

		storageIt := rawdb.IterateStorageSnapshots(clientDB.DiskDB(), accHash)
		defer storageIt.Release()

		snapshotStorageKeysCount := 0
		for storageIt.Next() {
			snapshotStorageKeysCount++
		}

		storageTrieLeavesCount := 0

		// check storage trie and storage snapshot consistency
		synctest.AssertTrieConsistency(t, acc.Root, serverDB.TrieDB(), clientDB.TrieDB(), func(key, val []byte) error {
			storageTrieLeavesCount++
			snapshotVal := rawdb.ReadStorageSnapshot(clientDB.DiskDB(), accHash, common.BytesToHash(key))
			require.Equal(t, val, snapshotVal)
			return nil
		})

		require.Equal(t, storageTrieLeavesCount, snapshotStorageKeysCount)
		return nil
	})

	// Check that the number of accounts in the snapshot matches the number of leaves in the accounts trie
	require.Equal(t, trieAccountLeaves, numSnapshotAccounts)
}

func fillAccountsWithStorage(t *testing.T, r *rand.Rand, serverDB state.Database, root common.Hash, numAccounts int) common.Hash {
	newRoot, _ := synctest.FillAccounts(t, r, serverDB, root, numAccounts, func(t *testing.T, _ int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
		codeBytes := make([]byte, 256)
		_, err := r.Read(codeBytes)
		require.NoError(t, err, "error reading random code bytes")

		codeHash := crypto.Keccak256Hash(codeBytes)
		rawdb.WriteCode(serverDB.DiskDB(), codeHash, codeBytes)
		account.CodeHash = codeHash[:]

		// now create state trie
		synctest.FillStorageForAccount(t, r, types.EmptyRootHash, 16, addr, storageTr)
		return account
	})
	return newRoot
}
