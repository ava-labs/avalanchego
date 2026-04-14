// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
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

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/message/messagetest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	handlerstats "github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
)

const testRequestSize = 1024

var errInterrupted = errors.New("interrupted sync")

// syncTestConfig holds optional configuration for testSyncWithConfig.
type syncTestConfig struct {
	ctx            context.Context
	wantError      error
	leafsIntercept func(message.LeafsRequest, message.LeafsResponse) (message.LeafsResponse, error)
	codeIntercept  func([]common.Hash, [][]byte) ([][]byte, error)
}

// testSync runs a full static sync and asserts DB consistency on success.
func testSync(t *testing.T, clientDB, serverDB state.Database, root common.Hash, c codec.Manager, leafReqType message.LeafsRequestType) {
	t.Helper()
	testSyncWithConfig(t, clientDB, serverDB, root, c, leafReqType, nil)
}

// testSyncWithConfig runs a static sync with optional intercepts, context, and
// expected error. If cfg is nil, defaults are used.
func testSyncWithConfig(t *testing.T, clientDB, serverDB state.Database, root common.Hash, c codec.Manager, leafReqType message.LeafsRequestType, cfg *syncTestConfig) {
	t.Helper()
	ctx := t.Context()
	var wantError error
	if cfg != nil {
		if cfg.ctx != nil {
			ctx = cfg.ctx
		}
		wantError = cfg.wantError
	}

	clientEthDB, ok := clientDB.DiskDB().(ethdb.Database)
	require.Truef(t, ok, "%T is not an ethdb.Database", clientDB.DiskDB())

	leafsHandler := handlers.NewLeafsRequestHandler(serverDB.TrieDB(), message.StateTrieKeyLength, nil, c, handlerstats.NewNoopHandlerStats())
	codeHandler := handlers.NewCodeRequestHandler(serverDB.DiskDB(), c, handlerstats.NewNoopHandlerStats())
	mockClient := client.NewTestClient(c, leafsHandler, codeHandler, nil)
	if cfg != nil {
		mockClient.GetLeafsIntercept = cfg.leafsIntercept
		mockClient.GetCodeIntercept = cfg.codeIntercept
	}

	queue, err := code.NewQueue(clientEthDB)
	require.NoError(t, err, "failed to create code queue")

	codeSyncer, err := code.NewSyncer(mockClient, clientEthDB, queue.CodeHashes())
	require.NoError(t, err, "failed to create code syncer")

	stateSyncer, err := NewHashDBSyncer(
		mockClient, clientEthDB, root, queue,
		testRequestSize, leafReqType,
		WithFinalizeCodeQueue(queue.Finalize),
		WithBatchSize(1000), // Use a lower batch size in order to get test coverage of batches being written early.
	)
	require.NoError(t, err, "failed to create state syncer")

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })

	err = eg.Wait()
	require.ErrorIs(t, err, wantError, "unexpected error during sync")

	if wantError != nil {
		return
	}

	synctest.AssertDBConsistency(t, root, clientDB, serverDB)
}

func TestSync(t *testing.T) {
	t.Run("accounts", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			r := rand.New(rand.NewSource(1))
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 250, nil)
			testSync(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType)
		})
	})

	t.Run("accounts with code", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			r := rand.New(rand.NewSource(1))
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 250, func(t *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
				if index%3 == 0 {
					codeBytes := make([]byte, 256)
					_, err := r.Read(codeBytes)
					require.NoError(t, err)
					codeHash := crypto.Keccak256Hash(codeBytes)
					rawdb.WriteCode(serverDB.DiskDB(), codeHash, codeBytes)
					account.CodeHash = codeHash[:]
				}
				return account
			})
			testSync(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType)
		})
	})

	t.Run("accounts with code and storage", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			r := rand.New(rand.NewSource(1))
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root, _ := synctest.FillAccountsWithStorageAndCode(t, r, serverDB, types.EmptyRootHash, 250)
			testSync(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType)
		})
	})

	t.Run("accounts with storage", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			r := rand.New(rand.NewSource(1))
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 250, func(t *testing.T, i int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
				if i%5 == 0 {
					synctest.FillStorageForAccount(t, r, 16, addr, storageTr)
				}
				return account
			})
			testSync(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType)
		})
	})

	t.Run("accounts with overlapping storage", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			r := rand.New(rand.NewSource(1))
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, 250, 3)
			testSync(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType)
		})
	})

	t.Run("failed to fetch leafs", func(t *testing.T) {
		t.Parallel()
		clientErr := errors.New("dummy client error")
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			r := rand.New(rand.NewSource(1))
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 10, nil)
			testSyncWithConfig(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType, &syncTestConfig{
				wantError: clientErr,
				leafsIntercept: func(_ message.LeafsRequest, _ message.LeafsResponse) (message.LeafsResponse, error) {
					return message.LeafsResponse{}, clientErr
				},
			})
		})
	})

	t.Run("failed to fetch code", func(t *testing.T) {
		t.Parallel()
		clientErr := errors.New("dummy client error")
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			r := rand.New(rand.NewSource(1))
			serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
			root, _ := synctest.FillAccountsWithStorageAndCode(t, r, serverDB, types.EmptyRootHash, 10)
			testSyncWithConfig(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType, &syncTestConfig{
				wantError: clientErr,
				codeIntercept: func(_ []common.Hash, _ [][]byte) ([][]byte, error) {
					return nil, clientErr
				},
			})
		})
	})
}

func TestCancelSync(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccountsWithStorageAndCode(t, r, serverDB, types.EmptyRootHash, 2000)

		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)

		testSyncWithConfig(t, state.NewDatabase(rawdb.NewMemoryDatabase()), serverDB, root, c, leafReqType, &syncTestConfig{
			ctx:       ctx,
			wantError: context.Canceled,
			leafsIntercept: func(_ message.LeafsRequest, lr message.LeafsResponse) (message.LeafsResponse, error) {
				cancel()
				return lr, nil
			},
		})
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
	if request.RootHash() == i.root {
		if numRequests := i.numRequests.Add(1); numRequests > i.interruptAfter {
			return message.LeafsResponse{}, errInterrupted
		}
	}
	return response, nil
}

func TestResumeSyncAccountsTrieInterrupted(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, 2000, 3)
		clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		intercept := &interruptLeafsIntercept{root: root, interruptAfter: 1}

		testSyncWithConfig(t, clientDB, serverDB, root, c, leafReqType, &syncTestConfig{
			wantError:      errInterrupted,
			leafsIntercept: intercept.getLeafsIntercept,
		})

		require.GreaterOrEqual(t, intercept.numRequests.Load(), uint32(2))

		testSync(t, clientDB, serverDB, root, c, leafReqType)
	})
}

func TestResumeSyncLargeStorageTrieInterrupted(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

		largeStorageRoot, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
		root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 2000, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
			if index == 10 {
				account.Root = largeStorageRoot
			}
			return account
		})
		clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		intercept := &interruptLeafsIntercept{root: largeStorageRoot, interruptAfter: 1}

		testSyncWithConfig(t, clientDB, serverDB, root, c, leafReqType, &syncTestConfig{
			wantError:      errInterrupted,
			leafsIntercept: intercept.getLeafsIntercept,
		})

		testSync(t, clientDB, serverDB, root, c, leafReqType)
	})
}

func TestResumeSyncToNewRootAfterLargeStorageTrieInterrupted(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

		largeStorageRoot1, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
		largeStorageRoot2, _, _ := synctest.GenerateIndependentTrie(t, r, serverDB.TrieDB(), 2000, common.HashLength)
		root1, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 2000, func(_ *testing.T, index int, _ common.Address, account types.StateAccount, _ state.Trie) types.StateAccount {
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
		intercept := &interruptLeafsIntercept{root: largeStorageRoot1, interruptAfter: 1}

		testSyncWithConfig(t, clientDB, serverDB, root1, c, leafReqType, &syncTestConfig{
			wantError:      errInterrupted,
			leafsIntercept: intercept.getLeafsIntercept,
		})

		<-snapshot.WipeSnapshot(clientDB.DiskDB(), false)

		testSync(t, clientDB, serverDB, root2, c, leafReqType)
	})
}

func TestResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterrupted(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
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
		intercept := &interruptLeafsIntercept{root: largeStorageRoot, interruptAfter: 1}

		testSyncWithConfig(t, clientDB, serverDB, root, c, leafReqType, &syncTestConfig{
			wantError:      errInterrupted,
			leafsIntercept: intercept.getLeafsIntercept,
		})

		testSync(t, clientDB, serverDB, root, c, leafReqType)
	})
}

func TestResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterrupted(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
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
		intercept := &interruptLeafsIntercept{root: largeStorageRoot, interruptAfter: 1}

		testSyncWithConfig(t, clientDB, serverDB, root, c, leafReqType, &syncTestConfig{
			wantError:      errInterrupted,
			leafsIntercept: intercept.getLeafsIntercept,
		})

		testSync(t, clientDB, serverDB, root, c, leafReqType)
	})
}

func TestResyncNewRootAfterDeletes(t *testing.T) {
	t.Run("delete code", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			testSyncerSyncsToNewRoot(t, deleteAllCode, c, leafReqType)
		})
	})
	t.Run("delete intermediate storage nodes", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			testSyncerSyncsToNewRoot(t, corruptStorageTries, c, leafReqType)
		})
	})
	t.Run("delete intermediate account trie nodes", func(t *testing.T) {
		t.Parallel()
		messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
			testSyncerSyncsToNewRoot(t, corruptAccountTrie, c, leafReqType)
		})
	})
}

func testSyncerSyncsToNewRoot(t *testing.T, deleteBetweenSyncs func(*testing.T, common.Hash, state.Database), c codec.Manager, leafReqType message.LeafsRequestType) {
	r := rand.New(rand.NewSource(1))
	clientDB := state.NewDatabase(rawdb.NewMemoryDatabase())
	serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	root1, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, common.Hash{}, 1000, 3)
	root2, _ := synctest.FillAccountsWithOverlappingStorage(t, r, serverDB, root1, 1000, 3)

	// Sync to root1.
	testSync(t, clientDB, serverDB, root1, c, leafReqType)

	// Wipe snapshot and corrupt state between syncs.
	<-snapshot.WipeSnapshot(clientDB.DiskDB(), false)
	deleteBetweenSyncs(t, root1, clientDB)

	// Re-sync to root2.
	testSync(t, clientDB, serverDB, root2, c, leafReqType)
}

// deleteAllCode removes all code and code-to-fetch markers from the client DB.
func deleteAllCode(t *testing.T, _ common.Hash, clientDB state.Database) {
	it := clientDB.DiskDB().NewIterator(rawdb.CodePrefix, nil)
	defer it.Release()
	for it.Next() {
		if len(it.Key()) != len(rawdb.CodePrefix)+common.HashLength {
			continue
		}
		require.NoError(t, clientDB.DiskDB().Delete(it.Key()), "failed to delete code hash %x", it.Key()[len(rawdb.CodePrefix):])
	}
	require.NoError(t, it.Error(), "error iterating over code hashes")

	codeToFetchIt := customrawdb.NewCodeToFetchIterator(clientDB.DiskDB())
	defer codeToFetchIt.Release()
	for codeToFetchIt.Next() {
		codeHash := common.BytesToHash(codeToFetchIt.Key()[len(customrawdb.CodeToFetchPrefix):])
		require.NoError(t, customrawdb.DeleteCodeToFetch(clientDB.DiskDB(), codeHash), "failed to delete code-to-fetch marker for hash %x", codeHash)
	}
	require.NoError(t, codeToFetchIt.Error(), "error iterating over code-to-fetch markers")
}

// corruptStorageTries deletes intermediate nodes from every other storage trie.
func corruptStorageTries(t *testing.T, root common.Hash, clientDB state.Database) {
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
}

// corruptAccountTrie deletes intermediate nodes from the account trie.
func corruptAccountTrie(t *testing.T, root common.Hash, clientDB state.Database) {
	clientTrieDB := clientDB.TrieDB()
	tr, err := trie.New(trie.TrieID(root), clientTrieDB)
	require.NoError(t, err, "failed to create trie for root %s", root)
	synctest.CorruptTrie(t, clientDB.DiskDB(), tr, 5)
}
