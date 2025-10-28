// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtest

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/coretest"
	"github.com/ava-labs/coreth/params/paramstest"
	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/vmsync"
	"github.com/ava-labs/coreth/sync/statesync/statesynctest"
	"github.com/ava-labs/coreth/utils/utilstest"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
)

type SyncerVMTest struct {
	Name     string
	TestFunc func(
		t *testing.T,
		testSetup *SyncTestSetup,
	)
}

var SyncerVMTests = []SyncerVMTest{
	{
		Name:     "SkipStateSyncTest",
		TestFunc: SkipStateSyncTest,
	},
	{
		Name:     "StateSyncFromScratchTest",
		TestFunc: StateSyncFromScratchTest,
	},
	{
		Name:     "StateSyncFromScratchExceedParentTest",
		TestFunc: StateSyncFromScratchExceedParentTest,
	},
	{
		Name:     "StateSyncToggleEnabledToDisabledTest",
		TestFunc: StateSyncToggleEnabledToDisabledTest,
	},
	{
		Name:     "VMShutdownWhileSyncingTest",
		TestFunc: VMShutdownWhileSyncingTest,
	},
}

func SkipStateSyncTest(t *testing.T, testSetup *SyncTestSetup) {
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 300, // must be greater than [syncableInterval] to skip sync
		SyncMode:           block.StateSyncSkipped,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, vmsync.BlocksToFetch, testSetup)

	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func StateSyncFromScratchTest(t *testing.T, testSetup *SyncTestSetup) {
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, vmsync.BlocksToFetch, testSetup)

	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func StateSyncFromScratchExceedParentTest(t *testing.T, testSetup *SyncTestSetup) {
	numToGen := vmsync.BlocksToFetch + uint64(32)
	test := SyncTestParams{
		SyncableInterval:   numToGen,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, int(numToGen), testSetup)

	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func StateSyncToggleEnabledToDisabledTest(t *testing.T, testSetup *SyncTestSetup) {
	var lock sync.Mutex
	require := require.New(t)
	reqCount := 0
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
		responseIntercept: func(syncerVM extension.InnerVM, nodeID ids.NodeID, requestID uint32, response []byte) {
			lock.Lock()
			defer lock.Unlock()

			reqCount++
			// Fail all requests after number 50 to interrupt the sync
			if reqCount > 50 {
				if err := syncerVM.AppRequestFailed(context.Background(), nodeID, requestID, commonEng.ErrTimeout); err != nil {
					panic(err)
				}
				if err := syncerVM.SyncerClient().Shutdown(); err != nil {
					panic(err)
				}
			} else {
				require.NoError(syncerVM.AppResponse(context.Background(), nodeID, requestID, response))
			}
		},
		expectedErr: context.Canceled,
	}
	testSyncVMSetup := initSyncServerAndClientVMs(t, test, vmsync.BlocksToFetch, testSetup)

	// Perform sync resulting in early termination.
	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)

	test.SyncMode = block.StateSyncStatic
	test.responseIntercept = nil
	test.expectedErr = nil

	syncDisabledVM, _ := testSetup.NewVM()
	appSender := &enginetest.Sender{T: t}
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
	appSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		require.True(hasItem, "expected nodeSet to contain at least 1 nodeID")
		go testSyncVMSetup.serverVM.VM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}
	ResetMetrics(testSyncVMSetup.syncerVM.SnowCtx)
	stateSyncDisabledConfigJSON := `{"state-sync-enabled":false}`
	genesisJSON := []byte(GenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest]))
	require.NoError(syncDisabledVM.Initialize(
		context.Background(),
		testSyncVMSetup.syncerVM.SnowCtx,
		testSyncVMSetup.syncerVM.DB,
		genesisJSON,
		nil,
		[]byte(stateSyncDisabledConfigJSON),
		[]*commonEng.Fx{},
		appSender,
	))

	defer func() {
		require.NoError(syncDisabledVM.Shutdown(context.Background()))
	}()

	require.Zero(syncDisabledVM.LastAcceptedExtendedBlock().Height(), "Unexpected last accepted height")

	enabled, err := syncDisabledVM.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled, "sync should be disabled")

	// Process the first 10 blocks from the serverVM
	for i := uint64(1); i < 10; i++ {
		ethBlock := testSyncVMSetup.serverVM.VM.Ethereum().BlockChain().GetBlockByNumber(i)
		require.NotNil(ethBlock, "couldn't get block %d", i)
		b, err := rlp.EncodeToBytes(ethBlock)
		require.NoError(err)
		blk, err := syncDisabledVM.ParseBlock(context.Background(), b)
		require.NoError(err)
		require.NoError(blk.Verify(context.Background()))
		require.NoError(blk.Accept(context.Background()))
	}
	// Verify the snapshot disk layer matches the last block root
	lastRoot := syncDisabledVM.Ethereum().BlockChain().CurrentBlock().Root
	require.NoError(syncDisabledVM.Ethereum().BlockChain().Snapshots().Verify(lastRoot))
	syncDisabledVM.Ethereum().BlockChain().DrainAcceptorQueue()

	// Create a new VM from the same database with state sync enabled.
	syncReEnabledVM, _ := testSetup.NewVM()
	// Enable state sync in configJSON
	configJSON := fmt.Sprintf(
		`{"state-sync-enabled":true, "state-sync-min-blocks":%d}`,
		test.StateSyncMinBlocks,
	)
	ResetMetrics(testSyncVMSetup.syncerVM.SnowCtx)
	require.NoError(syncReEnabledVM.Initialize(
		context.Background(),
		testSyncVMSetup.syncerVM.SnowCtx,
		testSyncVMSetup.syncerVM.DB,
		genesisJSON,
		nil,
		[]byte(configJSON),
		[]*commonEng.Fx{},
		appSender,
	))

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	testSyncVMSetup.serverVM.AppSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go syncReEnabledVM.AppResponse(ctx, nodeID, requestID, response)
		} else {
			go test.responseIntercept(syncReEnabledVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	require.NoError(syncReEnabledVM.Connected(
		context.Background(),
		testSyncVMSetup.serverVM.SnowCtx.NodeID,
		statesyncclient.StateSyncVersion,
	))

	enabled, err = syncReEnabledVM.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled, "sync should be enabled")

	testSyncVMSetup.syncerVM.VM = syncReEnabledVM
	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

func VMShutdownWhileSyncingTest(t *testing.T, testSetup *SyncTestSetup) {
	var (
		lock            sync.Mutex
		testSyncVMSetup *testSyncVMSetup
	)
	reqCount := 0
	test := SyncTestParams{
		SyncableInterval:   256,
		StateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		SyncMode:           block.StateSyncStatic,
		responseIntercept: func(syncerVM extension.InnerVM, nodeID ids.NodeID, requestID uint32, response []byte) {
			lock.Lock()
			defer lock.Unlock()

			reqCount++
			// Shutdown the VM after 50 requests to interrupt the sync
			if reqCount == 50 {
				// Note this verifies the VM shutdown does not time out while syncing.
				require.NoError(t, testSyncVMSetup.syncerVM.shutdownOnceSyncerVM.Shutdown(context.Background()))
			} else if reqCount < 50 {
				require.NoError(t, syncerVM.AppResponse(context.Background(), nodeID, requestID, response))
			}
		},
		expectedErr: context.Canceled,
	}
	testSyncVMSetup = initSyncServerAndClientVMs(t, test, vmsync.BlocksToFetch, testSetup)
	// Perform sync resulting in early termination.
	testSyncerVM(t, testSyncVMSetup, test, testSetup.ExtraSyncerVMTest)
}

type SyncTestSetup struct {
	NewVM             func() (extension.InnerVM, dummy.ConsensusCallbacks) // should not be initialized
	AfterInit         func(t *testing.T, testParams SyncTestParams, vmSetup SyncVMSetup, isServer bool)
	GenFn             func(i int, vm extension.InnerVM, gen *core.BlockGen)
	ExtraSyncerVMTest func(t *testing.T, syncerVM SyncVMSetup)
}

func initSyncServerAndClientVMs(t *testing.T, test SyncTestParams, numBlocks int, testSetup *SyncTestSetup) *testSyncVMSetup {
	require := require.New(t)

	// override commitInterval so the call to trie creates a commit at the height [syncableInterval].
	// This is necessary to support fetching a state summary.
	config := fmt.Sprintf(`{"commit-interval": %d, "state-sync-commit-interval": %d}`, test.SyncableInterval, test.SyncableInterval)
	serverVM, cb := testSetup.NewVM()
	fork := upgradetest.Latest
	serverTest := SetupTestVM(t, serverVM, TestVMConfig{
		Fork:       &fork,
		ConfigJSON: config,
	})
	t.Cleanup(func() {
		log.Info("Shutting down server VM")
		require.NoError(serverVM.Shutdown(context.Background()))
	})
	serverVMSetup := SyncVMSetup{
		VM:                 serverVM,
		AppSender:          serverTest.AppSender,
		SnowCtx:            serverTest.Ctx,
		ConsensusCallbacks: cb,
		DB:                 serverTest.DB,
		AtomicMemory:       serverTest.AtomicMemory,
	}
	var err error
	if testSetup.AfterInit != nil {
		testSetup.AfterInit(t, test, serverVMSetup, true)
	}
	generateAndAcceptBlocks(t, serverVM, numBlocks, testSetup.GenFn, nil, cb)

	// make some accounts
	r := rand.New(rand.NewSource(1))
	root, accounts := statesynctest.FillAccountsWithOverlappingStorage(t, r, serverVM.Ethereum().BlockChain().TrieDB(), types.EmptyRootHash, 1000, 16)

	// patch serverVM's lastAcceptedBlock to have the new root
	// and update the vm's state so the trie with accounts will
	// be returned by StateSyncGetLastSummary
	lastAccepted := serverVM.Ethereum().BlockChain().LastAcceptedBlock()
	patchedBlock := patchBlock(lastAccepted, root, serverVM.Ethereum().ChainDb())
	blockBytes, err := rlp.EncodeToBytes(patchedBlock)
	require.NoError(err)
	internalWrappedBlock, err := serverVM.ParseBlock(context.Background(), blockBytes)
	require.NoError(err)
	internalBlock, ok := internalWrappedBlock.(*chain.BlockWrapper)
	require.True(ok)
	require.NoError(serverVM.SetLastAcceptedBlock(internalBlock.Block))

	// initialise [syncerVM] with blank genesis state
	// we also override [syncerVM]'s commit interval so the atomic trie works correctly.
	stateSyncEnabledJSON := fmt.Sprintf(`{"state-sync-enabled":true, "state-sync-min-blocks": %d, "tx-lookup-limit": %d, "commit-interval": %d}`, test.StateSyncMinBlocks, 4, test.SyncableInterval)

	syncerVM, syncerCB := testSetup.NewVM()
	syncerTest := SetupTestVM(t, syncerVM, TestVMConfig{
		Fork:       &fork,
		ConfigJSON: stateSyncEnabledJSON,
		IsSyncing:  true,
	})
	shutdownOnceSyncerVM := &shutdownOnceVM{InnerVM: syncerVM}
	t.Cleanup(func() {
		require.NoError(shutdownOnceSyncerVM.Shutdown(context.Background()))
	})
	syncerVMSetup := syncerVMSetup{
		SyncVMSetup: SyncVMSetup{
			VM:                 syncerVM,
			ConsensusCallbacks: syncerCB,
			SnowCtx:            syncerTest.Ctx,
			DB:                 syncerTest.DB,
			AtomicMemory:       syncerTest.AtomicMemory,
		},
		shutdownOnceSyncerVM: shutdownOnceSyncerVM,
	}
	if testSetup.AfterInit != nil {
		testSetup.AfterInit(t, test, syncerVMSetup.SyncVMSetup, false)
	}
	require.NoError(syncerVM.SetState(context.Background(), snow.StateSyncing))
	enabled, err := syncerVM.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	serverTest.AppSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
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
			serverTest.Ctx.NodeID,
			statesyncclient.StateSyncVersion,
		),
	)

	// override [syncerVM]'s SendAppRequest function to trigger AppRequest on [serverVM]
	syncerTest.AppSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		require.True(hasItem, "expected nodeSet to contain at least 1 nodeID")
		require.NoError(serverVM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request))
		return nil
	}

	return &testSyncVMSetup{
		serverVM: SyncVMSetup{
			VM:        serverVM,
			AppSender: serverTest.AppSender,
			SnowCtx:   serverTest.Ctx,
		},
		fundedAccounts: accounts,
		syncerVM:       syncerVMSetup,
	}
}

// testSyncVMSetup contains the required set up for a client VM to perform state sync
// off of a server VM.
type testSyncVMSetup struct {
	serverVM SyncVMSetup
	syncerVM syncerVMSetup

	fundedAccounts map[*utilstest.Key]*types.StateAccount
}

type SyncVMSetup struct {
	VM                 extension.InnerVM
	SnowCtx            *snow.Context
	ConsensusCallbacks dummy.ConsensusCallbacks
	DB                 avalanchedatabase.Database
	AtomicMemory       *avalancheatomic.Memory
	AppSender          *enginetest.Sender
}

type syncerVMSetup struct {
	SyncVMSetup
	shutdownOnceSyncerVM *shutdownOnceVM
}

type shutdownOnceVM struct {
	extension.InnerVM
	shutdownOnce sync.Once
}

func (vm *shutdownOnceVM) Shutdown(ctx context.Context) error {
	var err error
	vm.shutdownOnce.Do(func() { err = vm.InnerVM.Shutdown(ctx) })
	return err
}

// SyncTestParams contains both the actual VMs as well as the parameters with the expected output.
type SyncTestParams struct {
	responseIntercept  func(vm extension.InnerVM, nodeID ids.NodeID, requestID uint32, response []byte)
	StateSyncMinBlocks uint64
	SyncableInterval   uint64
	SyncMode           block.StateSyncMode
	expectedErr        error
}

func testSyncerVM(t *testing.T, testSyncVMSetup *testSyncVMSetup, test SyncTestParams, extraSyncerVMTest func(t *testing.T, syncerVMSetup SyncVMSetup)) {
	t.Helper()
	var (
		require        = require.New(t)
		serverVM       = testSyncVMSetup.serverVM.VM
		fundedAccounts = testSyncVMSetup.fundedAccounts
		syncerVM       = testSyncVMSetup.syncerVM.VM
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
	require.Equal(test.SyncMode, syncMode)
	if syncMode == block.StateSyncSkipped {
		return
	}

	msg, err := syncerVM.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(commonEng.StateSyncDone, msg)

	// If the test is expected to error, require that the correct error is returned and finish the test.
	err = syncerVM.SyncerClient().Error()
	if test.expectedErr != nil {
		require.ErrorIs(err, test.expectedErr)
		// Note we re-open the database here to avoid a closed error when the test is for a shutdown VM.
		// TODO: this avoids circular dependencies but is not ideal.
		ethDBPrefix := []byte("ethdb")
		chaindb := database.New(prefixdb.NewNested(ethDBPrefix, testSyncVMSetup.syncerVM.DB))
		requireSyncPerformedHeights(t, chaindb, map[uint64]struct{}{})
		return
	}
	require.NoError(err, "state sync failed")

	// set [syncerVM] to bootstrapping and verify the last accepted block has been updated correctly
	// and that we can bootstrap and process some blocks.
	require.NoError(syncerVM.SetState(context.Background(), snow.Bootstrapping))
	require.Equal(serverVM.LastAcceptedExtendedBlock().Height(), syncerVM.LastAcceptedExtendedBlock().Height(), "block height mismatch between syncer and server")
	require.Equal(serverVM.LastAcceptedExtendedBlock().ID(), syncerVM.LastAcceptedExtendedBlock().ID(), "blockID mismatch between syncer and server")
	require.True(syncerVM.Ethereum().BlockChain().HasState(syncerVM.Ethereum().BlockChain().LastAcceptedBlock().Root()), "unavailable state for last accepted block")
	requireSyncPerformedHeights(t, syncerVM.Ethereum().ChainDb(), map[uint64]struct{}{retrievedSummary.Height(): {}})

	lastNumber := syncerVM.Ethereum().BlockChain().LastAcceptedBlock().NumberU64()
	// check the last block is indexed
	lastSyncedBlock := rawdb.ReadBlock(syncerVM.Ethereum().ChainDb(), rawdb.ReadCanonicalHash(syncerVM.Ethereum().ChainDb(), lastNumber), lastNumber)
	require.NotNil(lastSyncedBlock, "last synced block not found")
	for _, tx := range lastSyncedBlock.Transactions() {
		index := rawdb.ReadTxLookupEntry(syncerVM.Ethereum().ChainDb(), tx.Hash())
		require.NotNilf(index, "Miss transaction indices, number %d hash %s", lastNumber, tx.Hash().Hex())
	}

	// tail should be the last block synced
	if syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory != 0 {
		tail := lastSyncedBlock.NumberU64()

		coretest.CheckTxIndices(t, &tail, tail, tail, tail, syncerVM.Ethereum().ChainDb(), true)
	}

	blocksToBuild := 10
	txsPerBlock := 10
	toAddress := TestEthAddrs[1] // arbitrary choice
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, vm extension.InnerVM, gen *core.BlockGen) {
		br := predicate.BlockResults{}
		b, err := br.Bytes()
		require.NoError(err)
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, InitialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), k.PrivateKey)
			require.NoError(err)
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	},
		func(block *types.Block) {
			if syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory != 0 {
				tail := block.NumberU64() - syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory + 1
				// tail should be the minimum last synced block, since we skipped it to the last block
				if tail < lastSyncedBlock.NumberU64() {
					tail = lastSyncedBlock.NumberU64()
				}
				coretest.CheckTxIndices(t, &tail, tail, block.NumberU64(), block.NumberU64(), syncerVM.Ethereum().ChainDb(), true)
			}
		},
		testSyncVMSetup.syncerVM.ConsensusCallbacks,
	)

	// check we can transition to [NormalOp] state and continue to process blocks.
	require.NoError(syncerVM.SetState(context.Background(), snow.NormalOp))

	// Generate blocks after we have entered normal consensus as well
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, vm extension.InnerVM, gen *core.BlockGen) {
		br := predicate.BlockResults{}
		b, err := br.Bytes()
		require.NoError(err)
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, InitialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), k.PrivateKey)
			require.NoError(err)
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	},
		func(block *types.Block) {
			if syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory != 0 {
				tail := block.NumberU64() - syncerVM.Ethereum().BlockChain().CacheConfig().TransactionHistory + 1
				// tail should be the minimum last synced block, since we skipped it to the last block
				if tail < lastSyncedBlock.NumberU64() {
					tail = lastSyncedBlock.NumberU64()
				}
				coretest.CheckTxIndices(t, &tail, tail, block.NumberU64(), block.NumberU64(), syncerVM.Ethereum().ChainDb(), true)
			}
		},
		testSyncVMSetup.syncerVM.ConsensusCallbacks,
	)

	if extraSyncerVMTest != nil {
		extraSyncerVMTest(t, testSyncVMSetup.syncerVM.SyncVMSetup)
	}
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
	newBlk := customtypes.NewBlockWithExtData(
		header, blk.Transactions(), blk.Uncles(), receipts, trie.NewStackTrie(nil), customtypes.BlockExtData(blk), true,
	)
	rawdb.WriteBlock(db, newBlk)
	rawdb.WriteCanonicalHash(db, newBlk.Hash(), newBlk.NumberU64())
	return newBlk
}

// generateAndAcceptBlocks uses [core.GenerateChain] to generate blocks, then
// calls Verify and Accept on each generated block
// TODO: consider using this helper function in vm_test.go and elsewhere in this package to clean up tests
func generateAndAcceptBlocks(t *testing.T, vm extension.InnerVM, numBlocks int, gen func(int, extension.InnerVM, *core.BlockGen), accepted func(*types.Block), cb dummy.ConsensusCallbacks) {
	t.Helper()
	require := require.New(t)

	// acceptExternalBlock defines a function to parse, verify, and accept a block once it has been
	// generated by GenerateChain
	acceptExternalBlock := func(block *types.Block) {
		bytes, err := rlp.EncodeToBytes(block)
		require.NoError(err)
		extendedBlock, err := vm.ParseBlock(context.Background(), bytes)
		require.NoError(err)
		require.NoError(extendedBlock.Verify(context.Background()))
		require.NoError(extendedBlock.Accept(context.Background()))

		if accepted != nil {
			accepted(block)
		}
	}
	_, _, err := core.GenerateChain(
		vm.Ethereum().BlockChain().Config(),
		vm.Ethereum().BlockChain().LastAcceptedBlock(),
		dummy.NewFakerWithCallbacks(cb),
		vm.Ethereum().ChainDb(),
		numBlocks,
		10,
		func(i int, g *core.BlockGen) {
			g.SetOnBlockGenerated(acceptExternalBlock)
			g.SetCoinbase(constants.BlackholeAddr) // necessary for syntactic validation of the block
			gen(i, vm, g)
		},
	)
	require.NoError(err)
	vm.Ethereum().BlockChain().DrainAcceptorQueue()
}

// requireSyncPerformedHeights iterates over all heights the VM has synced to and
// verifies they all match the heights present in `expected`.
func requireSyncPerformedHeights(t *testing.T, db ethdb.Iteratee, expected map[uint64]struct{}) {
	it := customrawdb.NewSyncPerformedIterator(db)
	defer it.Release()

	found := make(map[uint64]struct{}, len(expected))
	for it.Next() {
		found[customrawdb.UnpackSyncPerformedKey(it.Key())] = struct{}{}
	}
	require.NoError(t, it.Error())
	require.Equal(t, expected, found)
}
