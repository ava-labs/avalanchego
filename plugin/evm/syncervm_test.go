// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/database"
	"github.com/ava-labs/coreth/predicate"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestSkipStateSync(t *testing.T) {
	rand.Seed(1)
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 300, // must be greater than [syncableInterval] to skip sync
		syncMode:           block.StateSyncSkipped,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, parentsToGet)

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncFromScratch(t *testing.T) {
	rand.Seed(1)
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		syncMode:           block.StateSyncStatic,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, parentsToGet)

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncFromScratchExceedParent(t *testing.T) {
	rand.Seed(1)
	numToGen := parentsToGet + uint64(32)
	test := syncTest{
		syncableInterval:   numToGen,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		syncMode:           block.StateSyncStatic,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, int(numToGen))

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncToggleEnabledToDisabled(t *testing.T) {
	rand.Seed(1)

	var lock sync.Mutex
	reqCount := 0
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		syncMode:           block.StateSyncStatic,
		responseIntercept: func(syncerVM *VM, nodeID ids.NodeID, requestID uint32, response []byte) {
			lock.Lock()
			defer lock.Unlock()

			reqCount++
			// Fail all requests after number 50 to interrupt the sync
			if reqCount > 50 {
				if err := syncerVM.AppRequestFailed(context.Background(), nodeID, requestID, commonEng.ErrTimeout); err != nil {
					panic(err)
				}
				cancel := syncerVM.StateSyncClient.(*stateSyncerClient).cancel
				if cancel != nil {
					cancel()
				} else {
					t.Fatal("state sync client not populated correctly")
				}
			} else {
				syncerVM.AppResponse(context.Background(), nodeID, requestID, response)
			}
		},
		expectedErr: context.Canceled,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, parentsToGet)

	// Perform sync resulting in early termination.
	testSyncerVM(t, vmSetup, test)

	test.syncMode = block.StateSyncStatic
	test.responseIntercept = nil
	test.expectedErr = nil

	syncDisabledVM := &VM{}
	appSender := &enginetest.Sender{T: t}
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
	appSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		if !hasItem {
			t.Fatal("expected nodeSet to contain at least 1 nodeID")
		}
		go vmSetup.serverVM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}
	// Reset metrics to allow re-initialization
	vmSetup.syncerVM.ctx.Metrics = metrics.NewPrefixGatherer()
	stateSyncDisabledConfigJSON := `{"state-sync-enabled":false}`
	if err := syncDisabledVM.Initialize(
		context.Background(),
		vmSetup.syncerVM.ctx,
		vmSetup.syncerDB,
		[]byte(genesisJSONLatest),
		nil,
		[]byte(stateSyncDisabledConfigJSON),
		vmSetup.syncerVM.toEngine,
		[]*commonEng.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := syncDisabledVM.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	if height := syncDisabledVM.LastAcceptedBlockInternal().Height(); height != 0 {
		t.Fatalf("Unexpected last accepted height: %d", height)
	}

	enabled, err := syncDisabledVM.StateSyncEnabled(context.Background())
	assert.NoError(t, err)
	assert.False(t, enabled, "sync should be disabled")

	// Process the first 10 blocks from the serverVM
	for i := uint64(1); i < 10; i++ {
		ethBlock := vmSetup.serverVM.blockChain.GetBlockByNumber(i)
		if ethBlock == nil {
			t.Fatalf("VM Server did not have a block available at height %d", i)
		}
		b, err := rlp.EncodeToBytes(ethBlock)
		if err != nil {
			t.Fatal(err)
		}
		blk, err := syncDisabledVM.ParseBlock(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		if err := blk.Verify(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := blk.Accept(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// Verify the snapshot disk layer matches the last block root
	lastRoot := syncDisabledVM.blockChain.CurrentBlock().Root
	if err := syncDisabledVM.blockChain.Snapshots().Verify(lastRoot); err != nil {
		t.Fatal(err)
	}
	syncDisabledVM.blockChain.DrainAcceptorQueue()

	// Create a new VM from the same database with state sync enabled.
	syncReEnabledVM := &VM{}
	// Enable state sync in configJSON
	configJSON := fmt.Sprintf(
		`{"state-sync-enabled":true, "state-sync-min-blocks":%d}`,
		test.stateSyncMinBlocks,
	)
	// Reset metrics to allow re-initialization
	vmSetup.syncerVM.ctx.Metrics = metrics.NewPrefixGatherer()
	if err := syncReEnabledVM.Initialize(
		context.Background(),
		vmSetup.syncerVM.ctx,
		vmSetup.syncerDB,
		[]byte(genesisJSONLatest),
		nil,
		[]byte(configJSON),
		vmSetup.syncerVM.toEngine,
		[]*commonEng.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	vmSetup.serverAppSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go syncReEnabledVM.AppResponse(ctx, nodeID, requestID, response)
		} else {
			go test.responseIntercept(syncReEnabledVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	assert.NoError(t, syncReEnabledVM.Connected(
		context.Background(),
		vmSetup.serverVM.ctx.NodeID,
		statesyncclient.StateSyncVersion,
	))

	enabled, err = syncReEnabledVM.StateSyncEnabled(context.Background())
	assert.NoError(t, err)
	assert.True(t, enabled, "sync should be enabled")

	vmSetup.syncerVM = syncReEnabledVM
	testSyncerVM(t, vmSetup, test)
}

func TestVMShutdownWhileSyncing(t *testing.T) {
	var (
		lock    sync.Mutex
		vmSetup *syncVMSetup
	)
	reqCount := 0
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		syncMode:           block.StateSyncStatic,
		responseIntercept: func(syncerVM *VM, nodeID ids.NodeID, requestID uint32, response []byte) {
			lock.Lock()
			defer lock.Unlock()

			reqCount++
			// Shutdown the VM after 50 requests to interrupt the sync
			if reqCount == 50 {
				// Note this verifies the VM shutdown does not time out while syncing.
				require.NoError(t, vmSetup.shutdownOnceSyncerVM.Shutdown(context.Background()))
			} else if reqCount < 50 {
				err := syncerVM.AppResponse(context.Background(), nodeID, requestID, response)
				require.NoError(t, err)
			}
		},
		expectedErr: context.Canceled,
	}
	vmSetup = createSyncServerAndClientVMs(t, test, parentsToGet)
	// Perform sync resulting in early termination.
	testSyncerVM(t, vmSetup, test)
}

func createSyncServerAndClientVMs(t *testing.T, test syncTest, numBlocks int) *syncVMSetup {
	var (
		require      = require.New(t)
		importAmount = 2000000 * units.Avax // 2M avax
		alloc        = map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		}
	)
	_, serverVM, _, serverAtomicMemory, serverAppSender := GenesisVMWithUTXOs(
		t, true, "", "", "", alloc,
	)
	t.Cleanup(func() {
		log.Info("Shutting down server VM")
		require.NoError(serverVM.Shutdown(context.Background()))
	})
	var (
		importTx, exportTx *atomic.Tx
		err                error
	)
	generateAndAcceptBlocks(t, serverVM, numBlocks, func(i int, gen *core.BlockGen) {
		b, err := predicate.NewResults().Bytes()
		if err != nil {
			t.Fatal(err)
		}
		gen.AppendExtra(b)
		switch i {
		case 0:
			// spend the UTXOs from shared memory
			importTx, err = serverVM.newImportTx(serverVM.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
			require.NoError(err)
			require.NoError(serverVM.mempool.AddLocalTx(importTx))
		case 1:
			// export some of the imported UTXOs to test exportTx is properly synced
			exportTx, err = serverVM.newExportTx(
				serverVM.ctx.AVAXAssetID,
				importAmount/2,
				serverVM.ctx.XChainID,
				testShortIDAddrs[0],
				initialBaseFee,
				[]*secp256k1.PrivateKey{testKeys[0]},
			)
			require.NoError(err)
			require.NoError(serverVM.mempool.AddLocalTx(exportTx))
		default: // Generate simple transfer transactions.
			pk := testKeys[0].ToECDSA()
			tx := types.NewTransaction(gen.TxNonce(testEthAddrs[0]), testEthAddrs[1], common.Big1, params.TxGas, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainID), pk)
			require.NoError(err)
			gen.AddTx(signedTx)
		}
	}, nil)

	// override serverAtomicTrie's commitInterval so the call to [serverAtomicTrie.Index]
	// creates a commit at the height [syncableInterval]. This is necessary to support
	// fetching a state summary.
	serverAtomicTrie := serverVM.atomicTrie.(*atomicTrie)
	serverAtomicTrie.commitInterval = test.syncableInterval
	require.NoError(serverAtomicTrie.commit(test.syncableInterval, serverAtomicTrie.LastAcceptedRoot()))
	require.NoError(serverVM.versiondb.Commit())

	serverSharedMemories := newSharedMemories(serverAtomicMemory, serverVM.ctx.ChainID, serverVM.ctx.XChainID)
	serverSharedMemories.assertOpsApplied(t, mustAtomicOps(importTx))
	serverSharedMemories.assertOpsApplied(t, mustAtomicOps(exportTx))

	// make some accounts
	trieDB := triedb.NewDatabase(serverVM.chaindb, nil)
	root, accounts := statesync.FillAccountsWithOverlappingStorage(t, trieDB, types.EmptyRootHash, 1000, 16)

	// patch serverVM's lastAcceptedBlock to have the new root
	// and update the vm's state so the trie with accounts will
	// be returned by StateSyncGetLastSummary
	lastAccepted := serverVM.blockChain.LastAcceptedBlock()
	patchedBlock := patchBlock(lastAccepted, root, serverVM.chaindb)
	blockBytes, err := rlp.EncodeToBytes(patchedBlock)
	require.NoError(err)
	internalBlock, err := serverVM.parseBlock(context.Background(), blockBytes)
	require.NoError(err)
	require.NoError(serverVM.State.SetLastAcceptedBlock(internalBlock))

	// patch syncableInterval for test
	serverVM.StateSyncServer.(*stateSyncServer).syncableInterval = test.syncableInterval

	// initialise [syncerVM] with blank genesis state
	stateSyncEnabledJSON := fmt.Sprintf(`{"state-sync-enabled":true, "state-sync-min-blocks": %d, "tx-lookup-limit": %d}`, test.stateSyncMinBlocks, 4)
	syncerEngineChan, syncerVM, syncerDB, syncerAtomicMemory, syncerAppSender := GenesisVMWithUTXOs(
		t, false, "", stateSyncEnabledJSON, "", alloc,
	)
	shutdownOnceSyncerVM := &shutdownOnceVM{VM: syncerVM}
	t.Cleanup(func() {
		require.NoError(shutdownOnceSyncerVM.Shutdown(context.Background()))
	})
	require.NoError(syncerVM.SetState(context.Background(), snow.StateSyncing))
	enabled, err := syncerVM.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)

	// override [syncerVM]'s commit interval so the atomic trie works correctly.
	syncerVM.atomicTrie.(*atomicTrie).commitInterval = test.syncableInterval

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	serverAppSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go syncerVM.AppResponse(ctx, nodeID, requestID, response)
		} else {
			go test.responseIntercept(syncerVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	require.NoError(
		syncerVM.Connected(
			context.Background(),
			serverVM.ctx.NodeID,
			statesyncclient.StateSyncVersion,
		),
	)

	// override [syncerVM]'s SendAppRequest function to trigger AppRequest on [serverVM]
	syncerAppSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		require.True(hasItem, "expected nodeSet to contain at least 1 nodeID")
		err := serverVM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request)
		require.NoError(err)
		return nil
	}

	return &syncVMSetup{
		serverVM:        serverVM,
		serverAppSender: serverAppSender,
		includedAtomicTxs: []*atomic.Tx{
			importTx,
			exportTx,
		},
		fundedAccounts:       accounts,
		syncerVM:             syncerVM,
		syncerDB:             syncerDB,
		syncerEngineChan:     syncerEngineChan,
		syncerAtomicMemory:   syncerAtomicMemory,
		shutdownOnceSyncerVM: shutdownOnceSyncerVM,
	}
}

// syncVMSetup contains the required set up for a client VM to perform state sync
// off of a server VM.
type syncVMSetup struct {
	serverVM        *VM
	serverAppSender *enginetest.Sender

	includedAtomicTxs []*atomic.Tx
	fundedAccounts    map[*keystore.Key]*types.StateAccount

	syncerVM             *VM
	syncerDB             avalanchedatabase.Database
	syncerEngineChan     <-chan commonEng.Message
	syncerAtomicMemory   *avalancheatomic.Memory
	shutdownOnceSyncerVM *shutdownOnceVM
}

type shutdownOnceVM struct {
	*VM
	shutdownOnce sync.Once
}

func (vm *shutdownOnceVM) Shutdown(ctx context.Context) error {
	var err error
	vm.shutdownOnce.Do(func() { err = vm.VM.Shutdown(ctx) })
	return err
}

// syncTest contains both the actual VMs as well as the parameters with the expected output.
type syncTest struct {
	responseIntercept  func(vm *VM, nodeID ids.NodeID, requestID uint32, response []byte)
	stateSyncMinBlocks uint64
	syncableInterval   uint64
	syncMode           block.StateSyncMode
	expectedErr        error
}

func testSyncerVM(t *testing.T, vmSetup *syncVMSetup, test syncTest) {
	t.Helper()
	var (
		require            = require.New(t)
		serverVM           = vmSetup.serverVM
		includedAtomicTxs  = vmSetup.includedAtomicTxs
		fundedAccounts     = vmSetup.fundedAccounts
		syncerVM           = vmSetup.syncerVM
		syncerEngineChan   = vmSetup.syncerEngineChan
		syncerAtomicMemory = vmSetup.syncerAtomicMemory
	)
	// get last summary and test related methods
	summary, err := serverVM.GetLastStateSummary(context.Background())
	require.NoError(err, "error getting state sync last summary")
	parsedSummary, err := syncerVM.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err, "error parsing state summary")
	retrievedSummary, err := serverVM.GetStateSummary(context.Background(), parsedSummary.Height())
	require.NoError(err, "error getting state sync summary at height")
	require.Equal(summary, retrievedSummary)

	syncMode, err := parsedSummary.Accept(context.Background())
	require.NoError(err, "error accepting state summary")
	require.Equal(test.syncMode, syncMode)
	if syncMode == block.StateSyncSkipped {
		return
	}

	msg := <-syncerEngineChan
	require.Equal(commonEng.StateSyncDone, msg)

	// If the test is expected to error, assert the correct error is returned and finish the test.
	err = syncerVM.StateSyncClient.Error()
	if test.expectedErr != nil {
		require.ErrorIs(err, test.expectedErr)
		// Note we re-open the database here to avoid a closed error when the test is for a shutdown VM.
		chaindb := database.WrapDatabase(prefixdb.NewNested(ethDBPrefix, syncerVM.db))
		assertSyncPerformedHeights(t, chaindb, map[uint64]struct{}{})
		return
	}
	require.NoError(err, "state sync failed")

	// set [syncerVM] to bootstrapping and verify the last accepted block has been updated correctly
	// and that we can bootstrap and process some blocks.
	require.NoError(syncerVM.SetState(context.Background(), snow.Bootstrapping))
	require.Equal(serverVM.LastAcceptedBlock().Height(), syncerVM.LastAcceptedBlock().Height(), "block height mismatch between syncer and server")
	require.Equal(serverVM.LastAcceptedBlock().ID(), syncerVM.LastAcceptedBlock().ID(), "blockID mismatch between syncer and server")
	require.True(syncerVM.blockChain.HasState(syncerVM.blockChain.LastAcceptedBlock().Root()), "unavailable state for last accepted block")
	assertSyncPerformedHeights(t, syncerVM.chaindb, map[uint64]struct{}{retrievedSummary.Height(): {}})

	lastNumber := syncerVM.blockChain.LastAcceptedBlock().NumberU64()
	// check the last block is indexed
	lastSyncedBlock := rawdb.ReadBlock(syncerVM.chaindb, rawdb.ReadCanonicalHash(syncerVM.chaindb, lastNumber), lastNumber)
	for _, tx := range lastSyncedBlock.Transactions() {
		index := rawdb.ReadTxLookupEntry(syncerVM.chaindb, tx.Hash())
		require.NotNilf(index, "Miss transaction indices, number %d hash %s", lastNumber, tx.Hash().Hex())
	}

	// tail should be the last block synced
	if syncerVM.ethConfig.TransactionHistory != 0 {
		tail := lastSyncedBlock.NumberU64()

		core.CheckTxIndices(t, &tail, tail, syncerVM.chaindb, true)
	}

	blocksToBuild := 10
	txsPerBlock := 10
	toAddress := testEthAddrs[1] // arbitrary choice
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, gen *core.BlockGen) {
		b, err := predicate.NewResults().Bytes()
		if err != nil {
			t.Fatal(err)
		}
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainConfig.ChainID), k.PrivateKey)
			require.NoError(err)
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	},
		func(block *types.Block) {
			if syncerVM.ethConfig.TransactionHistory != 0 {
				tail := block.NumberU64() - syncerVM.ethConfig.TransactionHistory + 1
				// tail should be the minimum last synced block, since we skipped it to the last block
				if tail < lastSyncedBlock.NumberU64() {
					tail = lastSyncedBlock.NumberU64()
				}
				core.CheckTxIndices(t, &tail, block.NumberU64(), syncerVM.chaindb, true)
			}
		},
	)

	// check we can transition to [NormalOp] state and continue to process blocks.
	require.NoError(syncerVM.SetState(context.Background(), snow.NormalOp))
	require.True(syncerVM.bootstrapped.Get())

	// check atomic memory was synced properly
	syncerSharedMemories := newSharedMemories(syncerAtomicMemory, syncerVM.ctx.ChainID, syncerVM.ctx.XChainID)

	for _, tx := range includedAtomicTxs {
		syncerSharedMemories.assertOpsApplied(t, mustAtomicOps(tx))
	}

	// Generate blocks after we have entered normal consensus as well
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, gen *core.BlockGen) {
		b, err := predicate.NewResults().Bytes()
		if err != nil {
			t.Fatal(err)
		}
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainConfig.ChainID), k.PrivateKey)
			require.NoError(err)
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	},
		func(block *types.Block) {
			if syncerVM.ethConfig.TransactionHistory != 0 {
				tail := block.NumberU64() - syncerVM.ethConfig.TransactionHistory + 1
				// tail should be the minimum last synced block, since we skipped it to the last block
				if tail < lastSyncedBlock.NumberU64() {
					tail = lastSyncedBlock.NumberU64()
				}
				core.CheckTxIndices(t, &tail, block.NumberU64(), syncerVM.chaindb, true)
			}
		},
	)
}

// patchBlock returns a copy of [blk] with [root] and updates [db] to
// include the new block as canonical for [blk]'s height.
// This breaks the digestibility of the chain since after this call
// [blk] does not necessarily define a state transition from its parent
// state to the new state root.
func patchBlock(blk *types.Block, root common.Hash, db ethdb.Database) *types.Block {
	header := blk.Header()
	header.Root = root
	receipts := rawdb.ReadRawReceipts(db, blk.Hash(), blk.NumberU64())
	newBlk := types.NewBlockWithExtData(
		header, blk.Transactions(), blk.Uncles(), receipts, trie.NewStackTrie(nil), blk.ExtData(), true,
	)
	rawdb.WriteBlock(db, newBlk)
	rawdb.WriteCanonicalHash(db, newBlk.Hash(), newBlk.NumberU64())
	return newBlk
}

// generateAndAcceptBlocks uses [core.GenerateChain] to generate blocks, then
// calls Verify and Accept on each generated block
// TODO: consider using this helper function in vm_test.go and elsewhere in this package to clean up tests
func generateAndAcceptBlocks(t *testing.T, vm *VM, numBlocks int, gen func(int, *core.BlockGen), accepted func(*types.Block)) {
	t.Helper()

	// acceptExternalBlock defines a function to parse, verify, and accept a block once it has been
	// generated by GenerateChain
	acceptExternalBlock := func(block *types.Block) {
		bytes, err := rlp.EncodeToBytes(block)
		if err != nil {
			t.Fatal(err)
		}
		vmBlock, err := vm.ParseBlock(context.Background(), bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Verify(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Accept(context.Background()); err != nil {
			t.Fatal(err)
		}

		if accepted != nil {
			accepted(block)
		}
	}
	_, _, err := core.GenerateChain(
		vm.chainConfig,
		vm.blockChain.LastAcceptedBlock(),
		dummy.NewFakerWithCallbacks(vm.createConsensusCallbacks()),
		vm.chaindb,
		numBlocks,
		10,
		func(i int, g *core.BlockGen) {
			g.SetOnBlockGenerated(acceptExternalBlock)
			g.SetCoinbase(constants.BlackholeAddr) // necessary for syntactic validation of the block
			gen(i, g)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.blockChain.DrainAcceptorQueue()
}

// assertSyncPerformedHeights iterates over all heights the VM has synced to and
// verifies it matches [expected].
func assertSyncPerformedHeights(t *testing.T, db ethdb.Iteratee, expected map[uint64]struct{}) {
	it := rawdb.NewSyncPerformedIterator(db)
	defer it.Release()

	found := make(map[uint64]struct{}, len(expected))
	for it.Next() {
		found[rawdb.UnpackSyncPerformedKey(it.Key())] = struct{}{}
	}
	require.NoError(t, it.Error())
	require.Equal(t, expected, found)
}
