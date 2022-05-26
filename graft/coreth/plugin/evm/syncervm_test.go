// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/ava-labs/coreth/accounts/keystore"
	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/statesync"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

func init() {
	// Set the max retry delay to 1 for these tests.
	defaultMaxRetryDelay = 1
}

func TestSkipStateSync(t *testing.T) {
	rand.Seed(1)
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 300, // must be greater than [syncableInterval] to skip sync
		shouldSync:         false,
	}
	vmSetup := createSyncServerAndClientVMs(t, test)
	defer vmSetup.Teardown(t)

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncFromScratch(t *testing.T) {
	rand.Seed(1)
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		shouldSync:         true,
	}
	vmSetup := createSyncServerAndClientVMs(t, test)
	defer vmSetup.Teardown(t)

	testSyncerVM(t, vmSetup, test)
}

func TestStateSyncToggleEnabledToDisabled(t *testing.T) {
	rand.Seed(1)
	// Hack: registering metrics uses global variables, so we need to disable metrics here so that we can initialize the VM twice.
	metrics.Enabled = false
	defer func() {
		metrics.Enabled = true
	}()

	var lock sync.Mutex
	reqCount := 0
	test := syncTest{
		syncableInterval:   256,
		stateSyncMinBlocks: 50, // must be less than [syncableInterval] to perform sync
		shouldSync:         true,
		responseIntercept: func(syncerVM *VM, nodeID ids.NodeID, requestID uint32, response []byte) {
			lock.Lock()
			defer lock.Unlock()

			reqCount++
			// Fail all requests after number 50 to interrupt the sync
			if reqCount > 50 {
				if err := syncerVM.AppRequestFailed(nodeID, requestID); err != nil {
					panic(err)
				}
			} else {
				syncerVM.AppResponse(nodeID, requestID, response)
			}
		},
		expectedErr: peer.ErrRequestFailed,
	}
	vmSetup := createSyncServerAndClientVMs(t, test)
	defer vmSetup.Teardown(t)

	// Perform sync resulting in early termination.
	testSyncerVM(t, vmSetup, test)

	test.shouldSync = true
	test.responseIntercept = nil
	test.expectedErr = nil

	syncDisabledVM := &VM{}
	appSender := &commonEng.SenderTest{T: t}
	appSender.SendAppGossipF = func([]byte) error { return nil }
	appSender.SendAppRequestF = func(nodeSet ids.NodeIDSet, requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		if !hasItem {
			t.Fatal("expected nodeSet to contain at least 1 nodeID")
		}
		go vmSetup.serverVM.AppRequest(nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}
	// Disable metrics to prevent duplicate registerer
	configJSON := "{\"metrics-enabled\":false}"
	if err := syncDisabledVM.Initialize(
		vmSetup.syncerVM.ctx,
		vmSetup.syncerDBManager,
		[]byte(genesisJSONApricotPhase5),
		nil,
		[]byte(configJSON),
		vmSetup.syncerVM.toEngine,
		[]*commonEng.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := syncDisabledVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	if height := syncDisabledVM.LastAcceptedBlockInternal().Height(); height != 0 {
		t.Fatalf("Unexpected last accepted height: %d", height)
	}

	enabled, err := syncDisabledVM.StateSyncEnabled()
	assert.NoError(t, err)
	assert.False(t, enabled, "sync should be disabled")

	// Process the first 10 blocks from the serverVM
	for i := uint64(1); i < 10; i++ {
		ethBlock := vmSetup.serverVM.chain.GetBlockByNumber(i)
		if ethBlock == nil {
			t.Fatalf("VM Server did not have a block available at height %d", i)
		}
		b, err := rlp.EncodeToBytes(ethBlock)
		if err != nil {
			t.Fatal(err)
		}
		blk, err := syncDisabledVM.ParseBlock(b)
		if err != nil {
			t.Fatal(err)
		}
		if err := blk.Verify(); err != nil {
			t.Fatal(err)
		}
		if err := blk.Accept(); err != nil {
			t.Fatal(err)
		}
	}
	// Verify the snapshot disk layer matches the last block root
	lastRoot := syncDisabledVM.chain.BlockChain().CurrentBlock().Root()
	if err := syncDisabledVM.chain.BlockChain().Snapshots().Verify(lastRoot); err != nil {
		t.Fatal(err)
	}

	// Create a new VM from the same database with state sync enabled.
	syncReEnabledVM := &VM{}
	// Disable metrics to prevent duplicate registerer
	configJSON = fmt.Sprintf(
		"{\"metrics-enabled\":false, \"state-sync-enabled\":true, \"state-sync-min-blocks\":%d}",
		test.stateSyncMinBlocks,
	)
	if err := syncReEnabledVM.Initialize(
		vmSetup.syncerVM.ctx,
		vmSetup.syncerDBManager,
		[]byte(genesisJSONApricotPhase5),
		nil,
		[]byte(configJSON),
		vmSetup.syncerVM.toEngine,
		[]*commonEng.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	vmSetup.serverAppSender.SendAppResponseF = func(nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go syncReEnabledVM.AppResponse(nodeID, requestID, response)
		} else {
			go test.responseIntercept(syncReEnabledVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	assert.NoError(t, syncReEnabledVM.Connected(vmSetup.serverVM.ctx.NodeID, statesyncclient.StateSyncVersion))

	enabled, err = syncReEnabledVM.StateSyncEnabled()
	assert.NoError(t, err)
	assert.True(t, enabled, "sync should be enabled")

	vmSetup.syncerVM = syncReEnabledVM
	testSyncerVM(t, vmSetup, test)
}

func createSyncServerAndClientVMs(t *testing.T, test syncTest) *syncVMSetup {
	var (
		serverVM, syncerVM *VM
	)
	// If there is an error shutdown the VMs if they have been instantiated
	defer func() {
		// If the test has not already failed, shut down the VMs since the caller
		// will not get the chance to shut them down.
		if !t.Failed() {
			return
		}

		// If the test already failed, shut down the VMs if they were instantiated.
		if serverVM != nil {
			log.Info("Shutting down server VM")
			if err := serverVM.Shutdown(); err != nil {
				t.Fatal(err)
			}
		}
		if syncerVM != nil {
			log.Info("Shutting down syncerVM")
			if err := syncerVM.Shutdown(); err != nil {
				t.Fatal(err)
			}
		}
	}()

	// configure [serverVM]
	importAmount := 2000000 * units.Avax // 2M avax
	_, serverVM, _, serverAtomicMemory, serverAppSender := GenesisVMWithUTXOs(
		t,
		true,
		genesisJSONApricotPhase5,
		"",
		"",
		map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	)

	var (
		importTx, exportTx *Tx
		err                error
	)
	generateAndAcceptBlocks(t, serverVM, parentsToGet, func(i int, gen *core.BlockGen) {
		switch i {
		case 0:
			// spend the UTXOs from shared memory
			importTx, err = serverVM.newImportTx(serverVM.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
			if err != nil {
				t.Fatal(err)
			}
			if err := serverVM.issueTx(importTx, true /*=local*/); err != nil {
				t.Fatal(err)
			}
		case 1:
			// export some of the imported UTXOs to test exportTx is properly synced
			exportTx, err = serverVM.newExportTx(
				serverVM.ctx.AVAXAssetID,
				importAmount/2,
				serverVM.ctx.XChainID,
				testShortIDAddrs[0],
				initialBaseFee,
				[]*crypto.PrivateKeySECP256K1R{testKeys[0]},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := serverVM.issueTx(exportTx, true /*=local*/); err != nil {
				t.Fatal(err)
			}
		default: // Generate simple transfer transactions.
			pk := testKeys[0].ToECDSA()
			tx := types.NewTransaction(gen.TxNonce(testEthAddrs[0]), testEthAddrs[1], common.Big1, params.TxGas, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainID), pk)
			if err != nil {
				t.Fatal(t)
			}
			gen.AddTx(signedTx)
		}
	})

	// override atomicTrie's commitHeightInterval so the call to [atomicTrie.Index]
	// creates a commit at the height [syncableInterval]. This is necessary to support
	// fetching a state summary.
	serverVM.atomicTrie.(*atomicTrie).commitHeightInterval = test.syncableInterval
	assert.NoError(t, serverVM.atomicTrie.Index(test.syncableInterval, nil))
	assert.NoError(t, serverVM.db.Commit())

	serverSharedMemories := newSharedMemories(serverAtomicMemory, serverVM.ctx.ChainID, serverVM.ctx.XChainID)
	serverSharedMemories.assertOpsApplied(t, importTx.mustAtomicOps())
	serverSharedMemories.assertOpsApplied(t, exportTx.mustAtomicOps())

	// make some accounts
	trieDB := trie.NewDatabase(serverVM.chaindb)
	root, accounts := statesync.FillAccountsWithOverlappingStorage(t, trieDB, types.EmptyRootHash, 1000, 16)

	// patch serverVM's lastAcceptedBlock to have the new root
	// and update the vm's state so the trie with accounts will
	// be returned by StateSyncGetLastSummary
	lastAccepted := serverVM.chain.LastAcceptedBlock()
	patchedBlock := patchBlock(lastAccepted, root, serverVM.chaindb)
	blockBytes, err := rlp.EncodeToBytes(patchedBlock)
	if err != nil {
		t.Fatal(err)
	}
	internalBlock, err := serverVM.parseBlock(blockBytes)
	if err != nil {
		t.Fatal(err)
	}
	internalBlock.(*Block).SetStatus(choices.Accepted)
	assert.NoError(t, serverVM.State.SetLastAcceptedBlock(internalBlock))

	// patch syncableInterval for test
	serverVM.StateSyncServer.(*stateSyncServer).syncableInterval = test.syncableInterval

	// initialise [syncerVM] with blank genesis state
	stateSyncEnabledJSON := fmt.Sprintf("{\"state-sync-enabled\":true, \"state-sync-min-blocks\": %d}", test.stateSyncMinBlocks)
	syncerEngineChan, syncerVM, syncerDBManager, syncerAtomicMemory, syncerAppSender := GenesisVMWithUTXOs(
		t,
		false,
		genesisJSONApricotPhase5,
		stateSyncEnabledJSON,
		"",
		map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	)
	if err := syncerVM.SetState(snow.StateSyncing); err != nil {
		t.Fatal(err)
	}
	enabled, err := syncerVM.StateSyncEnabled()
	assert.NoError(t, err)
	assert.True(t, enabled)

	// override [serverVM]'s SendAppResponse function to trigger AppResponse on [syncerVM]
	serverAppSender.SendAppResponseF = func(nodeID ids.NodeID, requestID uint32, response []byte) error {
		if test.responseIntercept == nil {
			go syncerVM.AppResponse(nodeID, requestID, response)
		} else {
			go test.responseIntercept(syncerVM, nodeID, requestID, response)
		}

		return nil
	}

	// connect peer to [syncerVM]
	assert.NoError(t, syncerVM.Connected(serverVM.ctx.NodeID, statesyncclient.StateSyncVersion))

	// override [syncerVM]'s SendAppRequest function to trigger AppRequest on [serverVM]
	syncerAppSender.SendAppRequestF = func(nodeSet ids.NodeIDSet, requestID uint32, request []byte) error {
		nodeID, hasItem := nodeSet.Pop()
		if !hasItem {
			t.Fatal("expected nodeSet to contain at least 1 nodeID")
		}
		go serverVM.AppRequest(nodeID, requestID, time.Now().Add(1*time.Second), request)
		return nil
	}

	return &syncVMSetup{
		serverVM:        serverVM,
		serverAppSender: serverAppSender,
		includedAtomicTxs: []*Tx{
			importTx,
			exportTx,
		},
		fundedAccounts:     accounts,
		syncerVM:           syncerVM,
		syncerDBManager:    syncerDBManager,
		syncerEngineChan:   syncerEngineChan,
		syncerAtomicMemory: syncerAtomicMemory,
	}
}

// syncVMSetup contains the required set up for a client VM to perform state sync
// off of a server VM.
type syncVMSetup struct {
	serverVM        *VM
	serverAppSender *commonEng.SenderTest

	includedAtomicTxs []*Tx
	fundedAccounts    map[*keystore.Key]*types.StateAccount

	syncerVM           *VM
	syncerDBManager    manager.Manager
	syncerEngineChan   <-chan commonEng.Message
	syncerAtomicMemory *atomic.Memory
}

// Teardown shuts down both VMs and asserts that both exit without error.
// Note: assumes both serverVM and sycnerVM have been initialized.
func (s *syncVMSetup) Teardown(t *testing.T) {
	assert.NoError(t, s.serverVM.Shutdown())
	assert.NoError(t, s.syncerVM.Shutdown())
}

// syncTest contains both the actual VMs as well as the parameters with the expected output.
type syncTest struct {
	responseIntercept  func(vm *VM, nodeID ids.NodeID, requestID uint32, response []byte)
	stateSyncMinBlocks uint64
	syncableInterval   uint64
	shouldSync         bool
	expectedErr        error
}

func testSyncerVM(t *testing.T, vmSetup *syncVMSetup, test syncTest) {
	var (
		serverVM           = vmSetup.serverVM
		includedAtomicTxs  = vmSetup.includedAtomicTxs
		fundedAccounts     = vmSetup.fundedAccounts
		syncerVM           = vmSetup.syncerVM
		syncerEngineChan   = vmSetup.syncerEngineChan
		syncerAtomicMemory = vmSetup.syncerAtomicMemory
	)

	// get last summary and test related methods
	summary, err := serverVM.GetLastStateSummary()
	if err != nil {
		t.Fatal("error getting state sync last summary", "err", err)
	}
	parsedSummary, err := syncerVM.ParseStateSummary(summary.Bytes())
	if err != nil {
		t.Fatal("error getting state sync last summary", "err", err)
	}
	retrievedSummary, err := serverVM.GetStateSummary(parsedSummary.Height())
	if err != nil {
		t.Fatal("error when checking if summary is accepted", "err", err)
	}
	assert.Equal(t, summary, retrievedSummary)

	shouldSync, err := parsedSummary.Accept()
	if err != nil {
		t.Fatal("unexpected error accepting state summary", "err", err)
	}
	if shouldSync != test.shouldSync {
		t.Fatal("unexpected value returned from accept", "expected", test.shouldSync, "got", shouldSync)
	}
	if !shouldSync {
		return
	}
	msg := <-syncerEngineChan
	assert.Equal(t, commonEng.StateSyncDone, msg)

	// If the test is expected to error, assert the correct error is returned and finish the test.
	err = syncerVM.StateSyncClient.Error()
	if test.expectedErr != nil {
		assert.ErrorIs(t, err, test.expectedErr)
		return
	}
	if err != nil {
		t.Fatal("state sync failed", err)
	}

	// set [syncerVM] to bootstrapping and verify the last accepted block has been updated correctly
	// and that we can bootstrap and process some blocks.
	if err := syncerVM.SetState(snow.Bootstrapping); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, serverVM.LastAcceptedBlock().Height(), syncerVM.LastAcceptedBlock().Height(), "block height mismatch between syncer and server")
	assert.Equal(t, serverVM.LastAcceptedBlock().ID(), syncerVM.LastAcceptedBlock().ID(), "blockID mismatch between syncer and server")
	assert.True(t, syncerVM.chain.BlockChain().HasState(syncerVM.chain.LastAcceptedBlock().Root()), "unavailable state for last accepted block")

	blocksToBuild := 10
	txsPerBlock := 10
	toAddress := testEthAddrs[2] // arbitrary choice
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, gen *core.BlockGen) {
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainID), k.PrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	})

	// check we can transition to [NormalOp] state and continue to process blocks.
	assert.NoError(t, syncerVM.SetState(snow.NormalOp))
	assert.True(t, syncerVM.bootstrapped)

	// check atomic memory was synced properly
	syncerSharedMemories := newSharedMemories(syncerAtomicMemory, syncerVM.ctx.ChainID, syncerVM.ctx.XChainID)

	for _, tx := range includedAtomicTxs {
		syncerSharedMemories.assertOpsApplied(t, tx.mustAtomicOps())
	}

	// Generate blocks after we have entered normal consensus as well
	generateAndAcceptBlocks(t, syncerVM, blocksToBuild, func(_ int, gen *core.BlockGen) {
		i := 0
		for k := range fundedAccounts {
			tx := types.NewTransaction(gen.TxNonce(k.Address), toAddress, big.NewInt(1), 21000, initialBaseFee, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(serverVM.chainID), k.PrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			gen.AddTx(signedTx)
			i++
			if i >= txsPerBlock {
				break
			}
		}
	})
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
		header, blk.Transactions(), blk.Uncles(), receipts, trie.NewStackTrie(nil), blk.ExtData(), true,
	)
	rawdb.WriteBlock(db, newBlk)
	rawdb.WriteCanonicalHash(db, newBlk.Hash(), newBlk.NumberU64())
	return newBlk
}

// generateAndAcceptBlocks uses [core.GenerateChain] to generate blocks, then
// calls Verify and Accept on each generated block
// TODO: consider using this helper function in vm_test.go and elsewhere in this package to clean up tests
func generateAndAcceptBlocks(t *testing.T, vm *VM, numBlocks int, gen func(int, *core.BlockGen)) {
	t.Helper()

	// acceptExternalBlock defines a function to parse, verify, and accept a block once it has been
	// generated by GenerateChain
	acceptExternalBlock := func(block *types.Block) {
		bytes, err := rlp.EncodeToBytes(block)
		if err != nil {
			t.Fatal(err)
		}
		vmBlock, err := vm.ParseBlock(bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Verify(); err != nil {
			t.Fatal(err)
		}
		if err := vmBlock.Accept(); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := core.GenerateChain(
		vm.chainConfig,
		vm.chain.LastAcceptedBlock(),
		dummy.NewDummyEngine(vm.createConsensusCallbacks()),
		vm.chaindb,
		numBlocks,
		10,
		func(i int, g *core.BlockGen) {
			g.SetOnBlockGenerated(acceptExternalBlock)
			g.SetCoinbase(coreth.BlackholeAddr) // necessary for syntactic validation of the block
			gen(i, g)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.chain.BlockChain().DrainAcceptorQueue()
}
