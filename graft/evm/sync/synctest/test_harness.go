// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"

	"github.com/ava-labs/avalanchego/graft/evm/sync/syncclient"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	handlerstats "github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"

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

	"github.com/ava-labs/avalanchego/graft/evm/message"
)

// TestRequestSize is the default request size for sync tests
const TestRequestSize = 1024

// errTestInterrupted is the error used to indicate test interruption
var errTestInterrupted = errors.New("interrupted sync")

// TestHarness provides the VM-specific dependencies needed to run state sync tests.
// This allows tests to be run from either graft/evm or graft/coreth packages.
type TestHarness struct {
	// SnapshotFactory creates a SnapshotIterable for the state syncer
	SnapshotFactory evmstate.SnapshotFactory

	// WipeSnapshot wipes the snapshot database. Takes the database and a bool for whether to wipe root.
	WipeSnapshot func(db ethdb.Database, root bool) <-chan struct{}
}

// SyncTestConfig configures a single sync test case
type SyncTestConfig struct {
	Ctx               context.Context
	PrepareForTest    func(t *testing.T, r *rand.Rand) (clientDB state.Database, serverDB state.Database, syncRoot common.Hash)
	ExpectedError     error
	GetLeafsIntercept func(message.LeafsRequest, message.LeafsResponse) (message.LeafsResponse, error)
	GetCodeIntercept  func([]common.Hash, [][]byte) ([][]byte, error)
}

// RunSyncTest runs a single sync test with the given harness and configuration
func RunSyncTest(t *testing.T, harness *TestHarness, test SyncTestConfig) {
	t.Helper()
	ctx := t.Context()
	if test.Ctx != nil {
		ctx = test.Ctx
	}
	r := rand.New(rand.NewSource(1))
	clientDB, serverDB, root := test.PrepareForTest(t, r)
	clientEthDB, ok := clientDB.DiskDB().(ethdb.Database)
	require.Truef(t, ok, "%T is not an ethdb.Database", clientDB.DiskDB())

	leafsRequestHandler := handlers.NewLeafsRequestHandler(serverDB.TrieDB(), message.StateTrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats())
	codeRequestHandler := handlers.NewCodeRequestHandler(serverDB.DiskDB(), message.Codec, handlerstats.NewNoopHandlerStats())
	mockClient := syncclient.NewTestClient(message.Codec, leafsRequestHandler, codeRequestHandler, nil)
	// Set intercept functions for the mock client
	mockClient.GetLeafsIntercept = test.GetLeafsIntercept
	mockClient.GetCodeIntercept = test.GetCodeIntercept

	// Create the code fetcher.
	fetcher, err := code.NewQueue(clientEthDB, make(chan struct{}))
	require.NoError(t, err, "failed to create code fetcher")

	// Create the consumer code syncer.
	codeSyncer, err := code.NewSyncer(mockClient, clientEthDB, fetcher.CodeHashes())
	require.NoError(t, err, "failed to create code syncer")

	// Create the state syncer with the harness-provided snapshot factory.
	stateSyncer, err := evmstate.NewSyncer(
		mockClient,
		clientEthDB,
		root,
		harness.SnapshotFactory,
		fetcher,
		TestRequestSize,
		evmstate.WithBatchSize(1000), // Use a lower batch size in order to get test coverage of batches being written early.
	)
	require.NoError(t, err, "failed to create state syncer")

	// Run both syncers concurrently and wait for the first error.
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })

	err = eg.Wait()
	require.ErrorIs(t, err, test.ExpectedError, "unexpected error during sync")

	// Only assert database consistency if the sync was expected to succeed.
	if test.ExpectedError != nil {
		return
	}

	AssertDBConsistency(t, root, clientDB, serverDB)
}

// RunSyncTestResumes runs a series of sync tests, invoking a callback after each step
func RunSyncTestResumes(t *testing.T, harness *TestHarness, steps []SyncTestConfig, stepCallback func()) {
	for _, test := range steps {
		RunSyncTest(t, harness, test)
		stepCallback()
	}
}

// InterruptLeafsIntercept provides parameters for getLeafsIntercept function
// which returns errInterrupted after passing through numRequests leafs requests for root.
type InterruptLeafsIntercept struct {
	NumRequests    atomic.Uint32
	InterruptAfter uint32
	Root           common.Hash
}

// GetLeafsIntercept can be passed to testClient and returns an unmodified
// response for the first [numRequest] requests for leafs from [root].
// After that, all requests for leafs from [root] return [errInterrupted].
func (i *InterruptLeafsIntercept) GetLeafsIntercept(request message.LeafsRequest, response message.LeafsResponse) (message.LeafsResponse, error) {
	if request.Root == i.Root {
		if numRequests := i.NumRequests.Add(1); numRequests > i.InterruptAfter {
			return message.LeafsResponse{}, errTestInterrupted
		}
	}
	return response, nil
}

// ErrInterrupted is the error returned when a sync is interrupted
var ErrInterrupted = errTestInterrupted

// AssertDBConsistency checks [serverTrieDB] and [clientTrieDB] have the same EVM state trie at [root],
// and that [clientTrieDB.DiskDB] has corresponding account & snapshot values.
// Also verifies any code referenced by the EVM state is present in [clientTrieDB] and the hash is correct.
func AssertDBConsistency(t testing.TB, root common.Hash, clientDB, serverDB state.Database) {
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

	AssertTrieConsistency(t, root, serverDB.TrieDB(), clientDB.TrieDB(), func(key, val []byte) error {
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
		AssertTrieConsistency(t, acc.Root, serverDB.TrieDB(), clientDB.TrieDB(), func(key, val []byte) error {
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

// GetSimpleSyncTestCases returns the standard sync test cases that can be run with any harness
func GetSimpleSyncTestCases(numAccounts, numAccountsSmall int) map[string]SyncTestConfig {
	clientErr := errors.New("dummy client error")
	return map[string]SyncTestConfig{
		"accounts": {
			PrepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := FillAccounts(t, r, serverDB, common.Hash{}, numAccounts, nil)
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
		},
		"accounts with code": {
			PrepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := FillAccounts(t, r, serverDB, common.Hash{}, numAccounts, func(t *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
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
			PrepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root := FillAccountsWithStorageAndCode(t, r, serverDB, numAccounts)
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
		},
		"accounts with storage": {
			PrepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := FillAccounts(t, r, serverDB, common.Hash{}, numAccounts, func(t *testing.T, i int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
					if i%5 == 0 {
						FillStorageForAccount(t, r, 16, addr, storageTr)
					}
					return account
				})
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
		},
		"accounts with overlapping storage": {
			PrepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, numAccounts, 3)
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
		},
		"failed to fetch leafs": {
			PrepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root, _ := FillAccounts(t, r, serverDB, common.Hash{}, numAccountsSmall, nil)
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
			GetLeafsIntercept: func(_ message.LeafsRequest, _ message.LeafsResponse) (message.LeafsResponse, error) {
				return message.LeafsResponse{}, clientErr
			},
			ExpectedError: clientErr,
		},
		"failed to fetch code": {
			PrepareForTest: func(t *testing.T, r *rand.Rand) (state.Database, state.Database, common.Hash) {
				serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
				root := FillAccountsWithStorageAndCode(t, r, serverDB, numAccountsSmall)
				return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
			},
			GetCodeIntercept: func(_ []common.Hash, _ [][]byte) ([][]byte, error) {
				return nil, clientErr
			},
			ExpectedError: clientErr,
		},
	}
}

// RunCancelSyncTest runs a test that cancels sync mid-operation
func RunCancelSyncTest(t *testing.T, harness *TestHarness) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	RunSyncTest(t, harness, SyncTestConfig{
		Ctx: ctx,
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			// Create trie with 2000 accounts (more than one leaf request)
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root := FillAccountsWithStorageAndCode(t, r, serverDB, 2000)
			return state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root
		},
		ExpectedError: context.Canceled,
		GetLeafsIntercept: func(_ message.LeafsRequest, lr message.LeafsResponse) (message.LeafsResponse, error) {
			cancel()
			return lr, nil
		},
	})
}

// RunResumeSyncAccountsTrieInterruptedTest tests resuming sync after accounts trie is interrupted
func RunResumeSyncAccountsTrieInterruptedTest(t *testing.T, harness *TestHarness) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	root, _ := FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, 2000, 3)
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &InterruptLeafsIntercept{
		Root:           root,
		InterruptAfter: 1,
	}
	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		ExpectedError:     ErrInterrupted,
		GetLeafsIntercept: intercept.GetLeafsIntercept,
	})

	require.GreaterOrEqual(t, intercept.NumRequests.Load(), uint32(2))

	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

// RunResumeSyncLargeStorageTrieInterruptedTest tests resuming sync after large storage trie is interrupted
func RunResumeSyncLargeStorageTrieInterruptedTest(t *testing.T, harness *TestHarness) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot, _, _ := GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root, _ := FillAccounts(t, r, serverDB, common.Hash{}, 2000, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		// Set the root for a single account
		if index == 10 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &InterruptLeafsIntercept{
		Root:           largeStorageRoot,
		InterruptAfter: 1,
	}
	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		ExpectedError:     ErrInterrupted,
		GetLeafsIntercept: intercept.GetLeafsIntercept,
	})

	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

// RunResumeSyncToNewRootAfterLargeStorageTrieInterruptedTest tests resuming to a new root
func RunResumeSyncToNewRootAfterLargeStorageTrieInterruptedTest(t *testing.T, harness *TestHarness) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot1, _, _ := GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	largeStorageRoot2, _, _ := GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root1, _ := FillAccounts(t, r, serverDB, common.Hash{}, 2000, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		if index == 10 {
			account.Root = largeStorageRoot1
		}
		return account
	})
	root2, _ := FillAccounts(t, r, serverDB, root1, 100, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		if index == 20 {
			account.Root = largeStorageRoot2
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &InterruptLeafsIntercept{
		Root:           largeStorageRoot1,
		InterruptAfter: 1,
	}
	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root1
		},
		ExpectedError:     ErrInterrupted,
		GetLeafsIntercept: intercept.GetLeafsIntercept,
	})

	<-harness.WipeSnapshot(clientDB.DiskDB().(ethdb.Database), false)

	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root2
		},
	})
}

// RunResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterruptedTest tests resuming with consecutive duplicates
func RunResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterruptedTest(t *testing.T, harness *TestHarness) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot, _, _ := GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root, _ := FillAccounts(t, r, serverDB, common.Hash{}, 100, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		if index == 10 || index == 11 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &InterruptLeafsIntercept{
		Root:           largeStorageRoot,
		InterruptAfter: 1,
	}
	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		ExpectedError:     ErrInterrupted,
		GetLeafsIntercept: intercept.GetLeafsIntercept,
	})

	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

// RunResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterruptedTest tests resuming with spread out duplicates
func RunResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterruptedTest(t *testing.T, harness *TestHarness) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	largeStorageRoot, _, _ := GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
	root, _ := FillAccounts(t, r, serverDB, common.Hash{}, 100, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
		if index == 10 || index == 90 {
			account.Root = largeStorageRoot
		}
		return account
	})
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	intercept := &InterruptLeafsIntercept{
		Root:           largeStorageRoot,
		InterruptAfter: 1,
	}
	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
		ExpectedError:     ErrInterrupted,
		GetLeafsIntercept: intercept.GetLeafsIntercept,
	})

	RunSyncTest(t, harness, SyncTestConfig{
		PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
			return clientDB, serverDB, root
		},
	})
}

// ResyncNewRootAfterDeletesTestCase defines a test case for resyncing after deletes
type ResyncNewRootAfterDeletesTestCase struct {
	Name               string
	DeleteBetweenSyncs func(t *testing.T, root common.Hash, clientDB state.Database)
}

// GetResyncNewRootAfterDeletesTestCases returns test cases for resyncing after deletes
func GetResyncNewRootAfterDeletesTestCases() []ResyncNewRootAfterDeletesTestCase {
	return []ResyncNewRootAfterDeletesTestCase{
		{
			Name: "delete code",
			DeleteBetweenSyncs: func(t *testing.T, _ common.Hash, clientDB state.Database) {
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
		{
			Name: "delete intermediate storage nodes",
			DeleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB state.Database) {
				clientTrieDB := clientDB.TrieDB()
				tr, err := trie.New(trie.TrieID(root), clientTrieDB)
				require.NoError(t, err, "failed to create trie for root %s", root)
				nodeIt, err := tr.NodeIterator(nil)
				require.NoError(t, err, "failed to create node iterator for root %s", root)
				it := trie.NewIterator(nodeIt)
				accountsWithStorage := 0

				corruptedStorageRoots := make(map[common.Hash]struct{})
				for it.Next() {
					var acc types.StateAccount
					require.NoError(t, rlp.DecodeBytes(it.Value, &acc), "failed to decode account at key %x", it.Key)
					if acc.Root == types.EmptyRootHash {
						continue
					}
					if _, found := corruptedStorageRoots[acc.Root]; found {
						continue
					}
					accountsWithStorage++
					if accountsWithStorage%2 != 0 {
						continue
					}
					corruptedStorageRoots[acc.Root] = struct{}{}
					tr, err := trie.New(trie.TrieID(acc.Root), clientTrieDB)
					require.NoError(t, err, "failed to create trie for root %s", acc.Root)
					CorruptTrie(t, clientDB.DiskDB(), tr, 2)
				}
				require.NoError(t, it.Err, "error iterating over trie nodes")
			},
		},
		{
			Name: "delete intermediate account trie nodes",
			DeleteBetweenSyncs: func(t *testing.T, root common.Hash, clientDB state.Database) {
				clientTrieDB := clientDB.TrieDB()
				tr, err := trie.New(trie.TrieID(root), clientTrieDB)
				require.NoError(t, err, "failed to create trie for root %s", root)
				CorruptTrie(t, clientDB.DiskDB(), tr, 5)
			},
		},
	}
}

// RunResyncNewRootAfterDeletesTest runs a resync test after deletes
func RunResyncNewRootAfterDeletesTest(t *testing.T, harness *TestHarness, testCase ResyncNewRootAfterDeletesTestCase) {
	r := rand.New(rand.NewSource(1))
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	root1, _ := FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, 1000, 3)
	root2, _ := FillAccountsWithOverlappingStorage(t, r, serverDB, root1, 1000, 3)

	called := false

	RunSyncTestResumes(t, harness, []SyncTestConfig{
		{
			PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
				return clientDB, serverDB, root1
			},
		},
		{
			PrepareForTest: func(*testing.T, *rand.Rand) (state.Database, state.Database, common.Hash) {
				return clientDB, serverDB, root2
			},
		},
	}, func() {
		if called {
			return
		}
		called = true
		<-harness.WipeSnapshot(clientDB.DiskDB().(ethdb.Database), false)
		testCase.DeleteBetweenSyncs(t, root1, clientDB)
	})
}
