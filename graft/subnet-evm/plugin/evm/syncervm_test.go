// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/coretest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/sync/statesync/statesynctest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	syncervm "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/sync"
	statesyncclient "github.com/ava-labs/avalanchego/graft/subnet-evm/sync/client"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestSkipStateSync(t *testing.T) {
	rand.Seed(1)
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 300, // must be greater than [syncableInterval] to skip sync
		syncMode:           block.StateSyncSkipped,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, syncervm.ParentsToFetch)

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncFromScratch(t *testing.T) {
	rand.Seed(1)
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		syncMode:           block.StateSyncStatic,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, syncervm.ParentsToFetch)

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncFromScratchExceedParent(t *testing.T) {
	rand.Seed(1)
	numToGen := syncervm.ParentsToFetch + uint64(32)
	test := syncTest{
		syncableInterval:   numToGen,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		syncMode:           block.StateSyncStatic,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, int(numToGen))

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncToggleEnabledToDisabled(t *testing.T) {
	rand.New(rand.NewSource(1))

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
				require.NoError(t, syncerVM.AppRequestFailed(t.Context(), nodeID, requestID, commonEng.ErrTimeout))
				require.NoError(t, syncerVM.Client.Shutdown())
			} else {
				require.NoError(t, syncerVM.AppResponse(t.Context(), nodeID, requestID, response))
			}
		},
		expectedErr: context.Canceled,
	}
	vmSetup := createSyncServerAndClientVMs(t, test, syncervm.ParentsToFetch)

	// Perform sync resulting in early termination.
	testSyncerVM(t, vmSetup, test)

	test.syncMode = block.StateSyncStatic
	test.responseIntercept = nil
	test.expectedErr = nil

	var atomicErr utils.Atomic[error]
	syncDisabledVM := &VM{}
	appSender := &enginetest.Sender{T: t}
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
	appSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		require.True(t, hasItem, "expected nodeSet to contain at least 1 nodeID")
		go func() {
			if err := vmSetup.serverVM.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request); err != nil {
				atomicErr.Set(err)
			}
		}()
		return nil
	}
	// Reset metrics to allow re-initialization
	vmSetup.syncerVM.ctx.Metrics = metrics.NewPrefixGatherer()
	stateSyncDisabledConfigJSON := `{"state-sync-enabled":false}`
	require.NoError(t, syncDisabledVM.Initialize(
		t.Context(),
		vmSetup.syncerVM.ctx,
		vmSetup.syncerDB,
		[]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest])),
		nil,
		[]byte(stateSyncDisabledConfigJSON),
		[]*commonEng.Fx{},
		appSender,
	))

	defer func() {
		require.NoError(t, syncDisabledVM.Shutdown(t.Context()))
	}()

	height := syncDisabledVM.LastAcceptedBlockInternal().Height()
	require.Zero(t, height, "Unexpected last accepted height: %d", height)

	enabled, err := syncDisabledVM.StateSyncEnabled(t.Context())
	require.NoError(t, err)
	require.False(t, enabled, "sync should be disabled")

	// Process the first 10 blocks from the serverVM
	for i := uint64(1); i < 10; i++ {
		ethBlock := vmSetup.serverVM.blockChain.GetBlockByNumber(i)
		require.NotNil(t, ethBlock, "VM Server did not have a block available at height %d", i)
		b, err := rlp.EncodeToBytes(ethBlock)
		require.NoError(t, err)
		blk, err := syncDisabledVM.ParseBlock(t.Context(), b)
		require.NoError(t, err)
		require.NoError(t, blk.Verify(t.Context()))
		require.NoError(t, blk.Accept(t.Context()))
	}
	// Verify the snapshot disk layer matches the last block root
	lastRoot := syncDisabledVM.blockChain.CurrentBlock().Root
	require.NoError(t, syncDisabledVM.blockChain.Snapshots().Verify(lastRoot))
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
	require.NoError(t, syncReEnabledVM.Initialize(
		t.Context(),
		vmSetup.syncerVM.ctx,
		vmSetup.syncerDB,
		[]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest])),
		nil,
		[]byte(configJSON),
		[]*commonEng.Fx{},
		appSender,
	))

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	vmSetup.serverAppSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go func() {
				if err := syncReEnabledVM.AppResponse(ctx, nodeID, requestID, response); err != nil {
					atomicErr.Set(err)
				}
			}()
		} else {
			go test.responseIntercept(syncReEnabledVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	require.NoError(t, syncReEnabledVM.Connected(
		t.Context(),
		vmSetup.serverVM.ctx.NodeID,
		statesyncclient.StateSyncVersion,
	))

	enabled, err = syncReEnabledVM.StateSyncEnabled(t.Context())
	require.NoError(t, err)
	require.True(t, enabled, "sync should be enabled")

	vmSetup.syncerVM = syncReEnabledVM
	testSyncerVM(t, vmSetup, test)
	require.NoError(t, atomicErr.Get())
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
				require.NoError(t, vmSetup.shutdownOnceSyncerVM.Shutdown(t.Context()))
			} else if reqCount < 50 {
				require.NoError(t, syncerVM.AppResponse(t.Context(), nodeID, requestID, response))
			}
		},
		expectedErr: context.Canceled,
	}
	vmSetup = createSyncServerAndClientVMs(t, test, syncervm.ParentsToFetch)
	// Perform sync resulting in early termination.
	testSyncerVM(t, vmSetup, test)
}

func createSyncServerAndClientVMs(t *testing.T, test syncTest, numBlocks int) *syncVMSetup {
	require := require.New(t)
	// configure [serverVM]
	// Align commit intervals with the test's syncable interval so summaries are created
	// at the expected heights and Accept() does not skip.
	serverConfigJSON := fmt.Sprintf(`{"commit-interval": %d, "state-sync-commit-interval": %d}`,
		test.syncableInterval, test.syncableInterval,
	)
	serverVM := newVM(t, testVMConfig{
		genesisJSON: toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest]),
		configJSON:  serverConfigJSON,
	})

	t.Cleanup(func() {
		log.Info("Shutting down server VM")
		require.NoError(serverVM.vm.Shutdown(t.Context()))
	})
	generateAndAcceptBlocks(t, serverVM.vm, numBlocks, func(_ int, gen *core.BlockGen) {
		br := predicate.BlockResults{}
		b, err := br.Bytes()
		require.NoError(err)
		gen.AppendExtra(b)

		tx := types.NewTransaction(gen.TxNonce(testEthAddrs[0]), testEthAddrs[1], common.Big1, ethparams.TxGas, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
		require.NoError(err, "failed to sign transaction")
		gen.AddTx(signedTx)
	}, nil)

	// make some accounts
	r := rand.New(rand.NewSource(1))
	trieDB := triedb.NewDatabase(serverVM.vm.chaindb, nil)
	root, accounts := statesynctest.FillAccountsWithOverlappingStorage(t, r, trieDB, types.EmptyRootHash, 1000, 16)

	// patch serverVM's lastAcceptedBlock to have the new root
	// and update the vm's state so the trie with accounts will
	// be returned by StateSyncGetLastSummary
	lastAccepted := serverVM.vm.blockChain.LastAcceptedBlock()
	patchedBlock := patchBlock(lastAccepted, root, serverVM.vm.chaindb)
	blockBytes, err := rlp.EncodeToBytes(patchedBlock)
	require.NoError(err)
	internalBlock, err := serverVM.vm.parseBlock(t.Context(), blockBytes)
	require.NoError(err)
	require.NoError(serverVM.vm.State.SetLastAcceptedBlock(internalBlock))

	// initialise [syncerVM] with blank genesis state
	// Match the server's state-sync-commit-interval so parsed summaries are acceptable.
	stateSyncEnabledJSON := fmt.Sprintf(
		`{"state-sync-enabled":true, "state-sync-min-blocks": %d, "tx-lookup-limit": %d, "state-sync-commit-interval": %d}`,
		test.stateSyncMinBlocks, 4, test.syncableInterval,
	)
	syncerVM := newVM(t, testVMConfig{
		genesisJSON: toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest]),
		configJSON:  stateSyncEnabledJSON,
		isSyncing:   true,
	})

	shutdownOnceSyncerVM := &shutdownOnceVM{VM: syncerVM.vm}
	t.Cleanup(func() {
		require.NoError(shutdownOnceSyncerVM.Shutdown(t.Context()))
	})
	require.NoError(syncerVM.vm.SetState(t.Context(), snow.StateSyncing))
	enabled, err := syncerVM.vm.StateSyncEnabled(t.Context())
	require.NoError(err)
	require.True(enabled)

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	var atomicErr utils.Atomic[error]
	serverVM.appSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go func() {
				if err := syncerVM.vm.AppResponse(ctx, nodeID, requestID, response); err != nil {
					atomicErr.Set(err)
				}
			}()
		} else {
			go test.responseIntercept(syncerVM.vm, nodeID, requestID, response)
		}

		return nil
	}
	t.Cleanup(func() {
		require.NoError(atomicErr.Get())
	})

	// connect peer to [syncerVM]
	require.NoError(
		syncerVM.vm.Connected(
			t.Context(),
			serverVM.vm.ctx.NodeID,
			statesyncclient.StateSyncVersion,
		),
	)

	// override [syncerVM]'s SendAppRequest function to trigger AppRequest on [serverVM]
	syncerVM.appSender.SendAppRequestF = func(ctx context.Context, nodeSet set.Set[ids.NodeID], requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		require.True(hasItem, "expected nodeSet to contain at least 1 nodeID")
		require.NoError(serverVM.vm.AppRequest(ctx, nodeID, requestID, time.Now().Add(1*time.Second), request))
		return nil
	}

	return &syncVMSetup{
		serverVM:             serverVM.vm,
		serverAppSender:      serverVM.appSender,
		fundedAccounts:       accounts,
		syncerVM:             syncerVM.vm,
		syncerDB:             syncerVM.db,
		shutdownOnceSyncerVM: shutdownOnceSyncerVM,
	}
}

// syncVMSetup contains the required set up for a client VM to perform state sync
// off of a server VM.
type syncVMSetup struct {
	serverVM        *VM
	serverAppSender *enginetest.Sender

	fundedAccounts map[*utilstest.Key]*types.StateAccount

	syncerVM             *VM
	syncerDB             avalanchedatabase.Database
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
		require        = require.New(t)
		serverVM       = vmSetup.serverVM
		fundedAccounts = vmSetup.fundedAccounts
		syncerVM       = vmSetup.syncerVM
	)
	// get last summary and test related methods
	summary, err := serverVM.GetLastStateSummary(t.Context())
	require.NoError(err, "error getting state sync last summary")
	parsedSummary, err := syncerVM.ParseStateSummary(t.Context(), summary.Bytes())
	require.NoError(err, "error parsing state summary")
	retrievedSummary, err := serverVM.GetStateSummary(t.Context(), parsedSummary.Height())
	require.NoError(err, "error getting state sync summary at height")
	require.Equal(summary, retrievedSummary)

	syncMode, err := parsedSummary.Accept(t.Context())
	require.NoError(err, "error accepting state summary")
	require.Equal(test.syncMode, syncMode)
	if syncMode == block.StateSyncSkipped {
		return
	}

	msg, err := syncerVM.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.StateSyncDone, msg)

	// If the test is expected to error, assert the correct error is returned and finish the test.
	err = syncerVM.Client.Error()
	if test.expectedErr != nil {
		require.ErrorIs(err, test.expectedErr)
		// Note we re-open the database here to avoid a closed error when the test is for a shutdown VM.
		chaindb := database.New(prefixdb.NewNested(ethDBPrefix, syncerVM.versiondb))
		assertSyncPerformedHeights(t, chaindb, map[uint64]struct{}{})
		return
	}
	require.NoError(err, "state sync failed")

	// set [syncerVM] to bootstrapping and verify the last accepted block has been updated correctly
	// and that we can bootstrap and process some blocks.
	require.NoError(syncerVM.SetState(t.Context(), snow.Bootstrapping))
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

		coretest.CheckTxIndices(t, &tail, tail, tail, tail, syncerVM.chaindb, true)
	}

	blocksToBuild := 10
	txsPerBlock := 10
	toAddress := testEthAddrs[1] // arbitrary choice
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, gen *core.BlockGen) {
		br := predicate.BlockResults{}
		b, err := br.Bytes()
		require.NoError(err)
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainConfig.ChainID), k.PrivateKey)
			require.NoError(err, "failed to sign transaction")
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
				coretest.CheckTxIndices(t, &tail, tail, block.NumberU64(), block.NumberU64(), syncerVM.chaindb, true)
			}
		},
	)

	// check we can transition to [NormalOp] state and continue to process blocks.
	require.NoError(syncerVM.SetState(t.Context(), snow.NormalOp))
	require.True(syncerVM.bootstrapped.Get())

	// Generate blocks after we have entered normal consensus as well
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, gen *core.BlockGen) {
		br := predicate.BlockResults{}
		b, err := br.Bytes()
		require.NoError(err)
		gen.AppendExtra(b)
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
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
				coretest.CheckTxIndices(t, &tail, tail, block.NumberU64(), block.NumberU64(), syncerVM.chaindb, true)
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
	newBlk := types.NewBlock(
		header, blk.Transactions(), blk.Uncles(), receipts, trie.NewStackTrie(nil),
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
		require.NoError(t, err)
		vmBlock, err := vm.ParseBlock(t.Context(), bytes)
		require.NoError(t, err)
		require.NoError(t, vmBlock.Verify(t.Context()))
		require.NoError(t, vmBlock.Accept(t.Context()))

		if accepted != nil {
			accepted(block)
		}
	}
	_, _, err := core.GenerateChain(
		vm.chainConfig,
		vm.blockChain.LastAcceptedBlock(),
		dummy.NewETHFaker(),
		vm.chaindb,
		numBlocks,
		10,
		func(i int, g *core.BlockGen) {
			g.SetOnBlockGenerated(acceptExternalBlock)
			g.SetCoinbase(constants.BlackholeAddr) // necessary for syntactic validation of the block
			gen(i, g)
		},
	)
	require.NoError(t, err)
	vm.blockChain.DrainAcceptorQueue()
}

// assertSyncPerformedHeights iterates over all heights the VM has synced to and
// verifies it matches [expected].
func assertSyncPerformedHeights(t *testing.T, db ethdb.Iteratee, expected map[uint64]struct{}) {
	it := customrawdb.NewSyncPerformedIterator(db)
	defer it.Release()

	found := make(map[uint64]struct{}, len(expected))
	for it.Next() {
		found[customrawdb.UnpackSyncPerformedKey(it.Key())] = struct{}{}
	}
	require.NoError(t, it.Error())
	require.Equal(t, expected, found)
}
