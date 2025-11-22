// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/customheader"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/gossip"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/plugin/evm/vmerrors"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/coreth/utils/rpc"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheutils "github.com/ava-labs/avalanchego/utils"
	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	atomicsync "github.com/ava-labs/coreth/plugin/evm/atomic/sync"
)

var (
	_ secp256k1fx.VM                     = (*VM)(nil)
	_ block.ChainVM                      = (*VM)(nil)
	_ block.BuildBlockWithContextChainVM = (*VM)(nil)
	_ block.StateSyncableVM              = (*VM)(nil)

	errAtomicGasExceedsLimit = errors.New("atomic gas used exceeds atomic gas limit")
)

const (
	secpCacheSize       = 1024
	defaultMempoolSize  = 4096
	targetAtomicTxsSize = 40 * units.KiB
	// maxAtomicTxMempoolGas is the maximum amount of gas that is allowed to be
	// used by an atomic transaction in the mempool. It is allowed to build
	// blocks with larger atomic transactions, but they will not be accepted
	// into the mempool.
	maxAtomicTxMempoolGas   = ap5.AtomicGasLimit
	atomicTxGossipNamespace = "atomic_tx_gossip"
	avaxEndpoint            = "/avax"
)

type VM struct {
	extension.InnerVM
	Ctx *snow.Context

	// TODO: unexport these fields
	SecpCache     *secp256k1.RecoverCache
	Fx            secp256k1fx.Fx
	baseCodec     codec.Registry
	AtomicMempool *txpool.Mempool

	// AtomicTxRepository maintains two indexes on accepted atomic txs.
	// - txID to accepted atomic tx
	// - block height to list of atomic txs accepted on block at that height
	// TODO: unexport these fields
	AtomicTxRepository *atomicstate.AtomicRepository
	// AtomicBackend abstracts verification and processing of atomic transactions
	AtomicBackend *atomicstate.AtomicBackend

	AtomicTxPushGossiper *avalanchegossip.PushGossiper[*atomic.Tx]

	// cancel may be nil until [snow.NormalOp] starts
	cancel     context.CancelFunc
	shutdownWg sync.WaitGroup

	clock        mockable.Clock
	bootstrapped avalancheutils.Atomic[bool]
}

func WrapVM(vm extension.InnerVM) *VM {
	return &VM{InnerVM: vm}
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db avalanchedatabase.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*avalanchecommon.Fx,
	appSender avalanchecommon.AppSender,
) error {
	vm.Ctx = chainCtx

	var extDataHashes map[common.Hash]common.Hash
	// Set the chain config for mainnet/fuji chain IDs
	switch chainCtx.NetworkID {
	case constants.MainnetID:
		extDataHashes = mainnetExtDataHashes
	case constants.FujiID:
		extDataHashes = fujiExtDataHashes
	}
	// Free the memory of the extDataHash map
	fujiExtDataHashes = nil
	mainnetExtDataHashes = nil

	// Create the atomic extension structs
	// some of them need to be initialized after the inner VM is initialized
	blockExtender := newBlockExtender(extDataHashes, vm)
	syncExtender := &atomicsync.Extender{}
	syncProvider := &atomicsync.SummaryProvider{}
	// Create and pass the leaf handler to the atomic extension
	// it will be initialized after the inner VM is initialized
	leafHandler := atomicsync.NewLeafHandler()
	atomicLeafTypeConfig := &extension.LeafRequestConfig{
		LeafType:   atomicsync.TrieNode,
		MetricName: "sync_atomic_trie_leaves",
		Handler:    leafHandler,
	}

	atomicTxs := txpool.NewTxs(chainCtx, defaultMempoolSize)
	extensionConfig := &extension.Config{
		ConsensusCallbacks:         vm.createConsensusCallbacks(),
		BlockExtender:              blockExtender,
		SyncableParser:             atomicsync.NewSummaryParser(),
		SyncExtender:               syncExtender,
		SyncSummaryProvider:        syncProvider,
		ExtraSyncLeafHandlerConfig: atomicLeafTypeConfig,
		ExtraMempool:               atomicTxs,
		Clock:                      &vm.clock,
	}
	if err := vm.InnerVM.SetExtensionConfig(extensionConfig); err != nil {
		return fmt.Errorf("failed to set extension config: %w", err)
	}

	// Initialize inner vm with the provided parameters
	if err := vm.InnerVM.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		fxs,
		appSender,
	); err != nil {
		return fmt.Errorf("failed to initialize inner VM: %w", err)
	}

	atomicMempool, err := txpool.NewMempool(atomicTxs, vm.InnerVM.MetricRegistry(), vm.verifyTxAtTip)
	if err != nil {
		return fmt.Errorf("failed to initialize mempool: %w", err)
	}
	vm.AtomicMempool = atomicMempool

	// initialize bonus blocks on mainnet
	var (
		bonusBlockHeights map[uint64]ids.ID
	)
	if vm.Ctx.NetworkID == constants.MainnetID {
		var err error
		bonusBlockHeights, err = readMainnetBonusBlocks()
		if err != nil {
			return fmt.Errorf("failed to read mainnet bonus blocks: %w", err)
		}
	}

	// initialize atomic repository
	lastAcceptedHash, lastAcceptedHeight, err := vm.InnerVM.ReadLastAccepted()
	if err != nil {
		return fmt.Errorf("failed to read last accepted block: %w", err)
	}
	vm.AtomicTxRepository, err = atomicstate.NewAtomicTxRepository(vm.InnerVM.VersionDB(), atomic.Codec, lastAcceptedHeight)
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}
	vm.AtomicBackend, err = atomicstate.NewAtomicBackend(
		vm.Ctx.SharedMemory, bonusBlockHeights,
		vm.AtomicTxRepository, lastAcceptedHeight, lastAcceptedHash,
		vm.InnerVM.Config().CommitInterval,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic backend: %w", err)
	}

	// Atomic backend is available now, we can initialize structs that depend on it
	atomicTrie := vm.AtomicBackend.AtomicTrie()
	syncProvider.Initialize(atomicTrie)
	syncExtender.Initialize(vm.AtomicBackend, atomicTrie, vm.InnerVM.Config().StateSyncRequestSize)
	leafHandler.Initialize(atomicTrie.TrieDB(), atomicstate.TrieKeyLength, message.Codec)

	vm.SecpCache = secp256k1.NewRecoverCache(secpCacheSize)

	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()
	return vm.Fx.Initialize(vm)
}

func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	switch state {
	case snow.StateSyncing:
		vm.bootstrapped.Set(false)
	case snow.Bootstrapping:
		if err := vm.onBootstrapStarted(); err != nil {
			return err
		}
	case snow.NormalOp:
		if err := vm.onNormalOperationsStarted(); err != nil {
			return err
		}
	}

	return vm.InnerVM.SetState(ctx, state)
}

func (vm *VM) onBootstrapStarted() error {
	vm.bootstrapped.Set(false)
	return vm.Fx.Bootstrapping()
}

func (vm *VM) onNormalOperationsStarted() error {
	if vm.bootstrapped.Get() {
		return nil
	}
	vm.bootstrapped.Set(true)
	if err := vm.Fx.Bootstrapped(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.TODO())
	vm.cancel = cancel
	atomicTxGossipMarshaller := atomic.TxMarshaller{}
	atomicTxGossipClient := vm.InnerVM.NewClient(p2p.AtomicTxGossipHandlerID)
	atomicTxGossipMetrics, err := avalanchegossip.NewMetrics(vm.InnerVM.MetricRegistry(), atomicTxGossipNamespace)
	if err != nil {
		return fmt.Errorf("failed to initialize atomic tx gossip metrics: %w", err)
	}

	pushGossipParams := avalanchegossip.BranchingFactor{
		StakePercentage: vm.InnerVM.Config().PushGossipPercentStake,
		Validators:      vm.InnerVM.Config().PushGossipNumValidators,
		Peers:           vm.InnerVM.Config().PushGossipNumPeers,
	}
	pushRegossipParams := avalanchegossip.BranchingFactor{
		Validators: vm.InnerVM.Config().PushRegossipNumValidators,
		Peers:      vm.InnerVM.Config().PushRegossipNumPeers,
	}

	vm.AtomicTxPushGossiper, err = avalanchegossip.NewPushGossiper[*atomic.Tx](
		&atomicTxGossipMarshaller,
		vm.AtomicMempool,
		vm.InnerVM.P2PValidators(),
		atomicTxGossipClient,
		atomicTxGossipMetrics,
		pushGossipParams,
		pushRegossipParams,
		config.PushGossipDiscardedElements,
		config.TxGossipTargetMessageSize,
		vm.InnerVM.Config().RegossipFrequency.Duration,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize atomic tx push gossiper: %w", err)
	}

	atomicTxGossipHandler, err := gossip.NewTxGossipHandler[*atomic.Tx](
		vm.Ctx.Log,
		&atomicTxGossipMarshaller,
		vm.AtomicMempool,
		atomicTxGossipMetrics,
		config.TxGossipTargetMessageSize,
		config.TxGossipThrottlingPeriod,
		config.TxGossipRequestsPerPeer,
		vm.InnerVM.P2PValidators(),
		vm.MetricRegistry(),
		"atomic_tx_gossip",
	)
	if err != nil {
		return fmt.Errorf("failed to initialize atomic tx gossip handler: %w", err)
	}

	if err := vm.InnerVM.AddHandler(p2p.AtomicTxGossipHandlerID, atomicTxGossipHandler); err != nil {
		return fmt.Errorf("failed to add atomic tx gossip handler: %w", err)
	}

	atomicTxPullGossiper := avalanchegossip.NewPullGossiper[*atomic.Tx](
		vm.Ctx.Log,
		&atomicTxGossipMarshaller,
		vm.AtomicMempool,
		atomicTxGossipClient,
		atomicTxGossipMetrics,
		config.TxGossipPollSize,
	)

	atomicTxPullGossiperWhenValidator := &avalanchegossip.ValidatorGossiper{
		Gossiper:   atomicTxPullGossiper,
		NodeID:     vm.Ctx.NodeID,
		Validators: vm.InnerVM.P2PValidators(),
	}

	vm.shutdownWg.Add(1)
	go func() {
		avalanchegossip.Every(ctx, vm.Ctx.Log, vm.AtomicTxPushGossiper, vm.InnerVM.Config().PushGossipFrequency.Duration)
		vm.shutdownWg.Done()
	}()

	vm.shutdownWg.Add(1)
	go func() {
		avalanchegossip.Every(ctx, vm.Ctx.Log, atomicTxPullGossiperWhenValidator, vm.InnerVM.Config().PullGossipFrequency.Duration)
		vm.shutdownWg.Done()
	}()

	return nil
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.Ctx == nil {
		return nil
	}
	if vm.cancel != nil {
		vm.cancel()
	}
	if err := vm.InnerVM.Shutdown(context.Background()); err != nil {
		log.Error("failed to shutdown inner VM", "err", err)
	}
	vm.shutdownWg.Wait()
	return nil
}

func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	apis, err := vm.InnerVM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}
	avaxAPI, err := rpc.NewHandler("avax", &AvaxAPI{vm})
	if err != nil {
		return nil, fmt.Errorf("failed to register service for AVAX API due to %w", err)
	}
	log.Info("AVAX API enabled")
	apis[avaxEndpoint] = avaxAPI
	return apis, nil
}

// verifyTxAtTip verifies that [tx] is valid to be issued on top of the currently preferred block
func (vm *VM) verifyTxAtTip(tx *atomic.Tx) error {
	if txByteLen := len(tx.SignedBytes()); txByteLen > targetAtomicTxsSize {
		return fmt.Errorf("tx size (%d) exceeds total atomic txs size target (%d)", txByteLen, targetAtomicTxsSize)
	}
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return err
	}
	if gasUsed > maxAtomicTxMempoolGas {
		return fmt.Errorf("tx gas usage (%d) exceeds maximum allowed mempool gas usage (%d)", gasUsed, maxAtomicTxMempoolGas)
	}
	blockchain := vm.InnerVM.Ethereum().BlockChain()
	// Note: we fetch the current block and then the state at that block instead of the current state directly
	// since we need the header of the current block below.
	preferredBlock := blockchain.CurrentBlock()
	preferredState, err := blockchain.StateAt(preferredBlock.Root)
	if err != nil {
		return fmt.Errorf("failed to retrieve block state at tip while verifying atomic tx: %w", err)
	}
	extraConfig := params.GetExtra(vm.InnerVM.ChainConfig())
	extraRules := params.GetRulesExtra(vm.InnerVM.ChainConfig().Rules(preferredBlock.Number, params.IsMergeTODO, preferredBlock.Time))
	parentHeader := preferredBlock
	var nextBaseFee *big.Int
	now := vm.clock.Time()
	timestamp := uint64(now.Unix())
	if extraConfig.IsApricotPhase3(timestamp) {
		timeMS := uint64(now.UnixMilli())
		nextBaseFee, err = customheader.EstimateNextBaseFee(extraConfig, parentHeader, timeMS)
		if err != nil {
			// Return extremely detailed error since CalcBaseFee should never encounter an issue here
			return fmt.Errorf("failed to calculate base fee with parent timestamp (%d), parent ExtraData: (0x%x), and current timestamp (%d): %w", parentHeader.Time, parentHeader.Extra, timestamp, err)
		}
	}

	// We don’t need to revert the state here in case verifyTx errors, because
	// preferredState is thrown away either way.
	return vm.verifyTx(tx, parentHeader.Hash(), nextBaseFee, preferredState, *extraRules)
}

// verifyTx verifies that [tx] is valid to be issued into a block with parent block [parentHash]
// and validated at [state] using [rules] as the current rule set.
// Note: VerifyTx may modify [state]. If [state] needs to be properly maintained, the caller is responsible
// for reverting to the correct snapshot after calling this function. If this function is called with a
// throwaway state, then this is not necessary.
// TODO: unexport this function
func (vm *VM) verifyTx(tx *atomic.Tx, parentHash common.Hash, baseFee *big.Int, statedb *state.StateDB, rules extras.Rules) error {
	parent, err := vm.InnerVM.GetExtendedBlock(context.TODO(), ids.ID(parentHash))
	if err != nil {
		return fmt.Errorf("failed to get parent block: %w", err)
	}
	verifierBackend := NewVerifierBackend(vm, rules)
	if err := verifierBackend.SemanticVerify(tx, parent, baseFee); err != nil {
		return err
	}
	wrappedStateDB := extstate.New(statedb)
	return tx.UnsignedAtomicTx.EVMStateTransfer(vm.Ctx, wrappedStateDB)
}

// verifyTxs verifies that [txs] are valid to be issued into a block with parent block [parentHash]
// using [rules] as the current rule set.
func (vm *VM) verifyTxs(txs []*atomic.Tx, parentHash common.Hash, baseFee *big.Int, height uint64, rules extras.Rules) error {
	// Ensure that the parent was verified and inserted correctly.
	if !vm.InnerVM.Ethereum().BlockChain().HasBlock(parentHash, height-1) {
		return errRejectedParent
	}

	ancestorID := ids.ID(parentHash)
	// If the ancestor is unknown, then the parent failed verification when
	// it was called.
	// If the ancestor is rejected, then this block shouldn't be inserted
	// into the canonical chain because the parent will be missing.
	ancestor, err := vm.InnerVM.GetExtendedBlock(context.TODO(), ancestorID)
	if err != nil {
		return errRejectedParent
	}

	// Ensure each tx in [txs] doesn't conflict with any other atomic tx in
	// a processing ancestor block.
	inputs := set.Set[ids.ID]{}
	verifierBackend := NewVerifierBackend(vm, rules)

	for _, atomicTx := range txs {
		utx := atomicTx.UnsignedAtomicTx
		if err := verifierBackend.SemanticVerify(atomicTx, ancestor, baseFee); err != nil {
			return fmt.Errorf("invalid block due to failed semantic verify: %w at height %d", err, height)
		}
		txInputs := utx.InputUTXOs()
		if inputs.Overlaps(txInputs) {
			return ErrConflictingAtomicInputs
		}
		inputs.Union(txInputs)
	}
	return nil
}

// CodecRegistry implements the secp256k1fx interface
func (vm *VM) CodecRegistry() codec.Registry { return vm.baseCodec }

// Clock implements the secp256k1fx interface
func (vm *VM) Clock() *mockable.Clock { return &vm.clock }

// Logger implements the secp256k1fx interface
func (vm *VM) Logger() logging.Logger { return vm.Ctx.Log }

func (vm *VM) createConsensusCallbacks() dummy.ConsensusCallbacks {
	return dummy.ConsensusCallbacks{
		OnFinalizeAndAssemble: vm.onFinalizeAndAssemble,
		OnExtraStateChange:    vm.onExtraStateChange,
	}
}

func (vm *VM) preBatchOnFinalizeAndAssemble(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	for {
		tx, exists := vm.AtomicMempool.NextTx()
		if !exists {
			break
		}
		// Take a snapshot of [state] before calling verifyTx so that if the transaction fails verification
		// we can revert to [snapshot].
		// Note: snapshot is taken inside the loop because you cannot revert to the same snapshot more than
		// once.
		snapshot := state.Snapshot()
		rules := vm.rules(header.Number, header.Time)
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx from mempool on failed verification", "txID", tx.ID(), "err", err)
			vm.AtomicMempool.DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, tx)
		if err != nil {
			// Discard the transaction from the mempool and error if the transaction
			// cannot be marshalled. This should never happen.
			log.Debug("discarding tx due to unmarshal err", "txID", tx.ID(), "err", err)
			vm.AtomicMempool.DiscardCurrentTx(tx.ID())
			return nil, nil, nil, fmt.Errorf("failed to marshal atomic transaction %s due to %w", tx.ID(), err)
		}
		var contribution, gasUsed *big.Int
		if rules.IsApricotPhase4 {
			contribution, gasUsed, err = tx.BlockFeeContribution(rules.IsApricotPhase5, vm.Ctx.AVAXAssetID, header.BaseFee)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		return atomicTxBytes, contribution, gasUsed, nil
	}

	if len(txs) == 0 {
		// this could happen due to the async logic of geth tx pool
		return nil, nil, nil, ErrEmptyBlock
	}

	return nil, nil, nil, nil
}

// assumes that we are in at least Apricot Phase 5.
func (vm *VM) postBatchOnFinalizeAndAssemble(
	header *types.Header,
	parent *types.Header,
	state *state.StateDB,
	txs []*types.Transaction,
) ([]byte, *big.Int, *big.Int, error) {
	var (
		batchAtomicTxs    []*atomic.Tx
		batchAtomicUTXOs  set.Set[ids.ID]
		batchContribution = new(big.Int).Set(common.Big0)
		batchGasUsed      = new(big.Int).Set(common.Big0)
		rules             = vm.rules(header.Number, header.Time)
		size              int
	)

	atomicGasLimit, err := customheader.RemainingAtomicGasCapacity(vm.chainConfigExtra(), parent, header)
	if err != nil {
		return nil, nil, nil, err
	}

	for {
		tx, exists := vm.AtomicMempool.NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size soft limit.
		txSize := len(tx.SignedBytes())
		if size+txSize > targetAtomicTxsSize {
			vm.AtomicMempool.CancelCurrentTx(tx.ID())
			break
		}

		var (
			txGasUsed, txContribution *big.Int
			err                       error
		)

		// Note: we do not need to check if we are in at least ApricotPhase4 here because
		// we assume that this function will only be called when the block is in at least
		// ApricotPhase5.
		txContribution, txGasUsed, err = tx.BlockFeeContribution(true, vm.Ctx.AVAXAssetID, header.BaseFee)
		if err != nil {
			return nil, nil, nil, err
		}
		// ensure `gasUsed + batchGasUsed` doesn't exceed `atomicGasLimit`
		if totalGasUsed := new(big.Int).Add(batchGasUsed, txGasUsed); !utils.BigLessOrEqualUint64(totalGasUsed, atomicGasLimit) {
			// Send [tx] back to the mempool's tx heap.
			vm.AtomicMempool.CancelCurrentTx(tx.ID())
			break
		}

		if batchAtomicUTXOs.Overlaps(tx.InputUTXOs()) {
			// Discard the transaction from the mempool since it will fail verification
			// after this block has been accepted.
			// Note: if the proposed block is not accepted, the transaction may still be
			// valid, but we discard it early here based on the assumption that the proposed
			// block will most likely be accepted.
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx due to overlapping input utxos", "txID", tx.ID())
			vm.AtomicMempool.DiscardCurrentTx(tx.ID())
			continue
		}

		snapshot := state.Snapshot()
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool and reset the state to [snapshot]
			// if it fails verification here.
			// Note: prior to this point, we have not modified [state] so there is no need to
			// revert to a snapshot if we discard the transaction prior to this point.
			log.Debug("discarding tx from mempool due to failed verification", "txID", tx.ID(), "err", err)
			vm.AtomicMempool.DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		batchAtomicTxs = append(batchAtomicTxs, tx)
		batchAtomicUTXOs.Union(tx.InputUTXOs())
		// Add the [txGasUsed] to the [batchGasUsed] when the [tx] has passed verification
		batchGasUsed.Add(batchGasUsed, txGasUsed)
		batchContribution.Add(batchContribution, txContribution)
		size += txSize
	}

	// If there is a non-zero number of transactions, marshal them and return the byte slice
	// for the block's extra data along with the contribution and gas used.
	if len(batchAtomicTxs) > 0 {
		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, batchAtomicTxs)
		if err != nil {
			// If we fail to marshal the batch of atomic transactions for any reason,
			// discard the entire set of current transactions.
			log.Debug("discarding txs due to error marshaling atomic transactions", "err", err)
			vm.AtomicMempool.DiscardCurrentTxs()
			return nil, nil, nil, fmt.Errorf("failed to marshal batch of atomic transactions due to %w", err)
		}
		return atomicTxBytes, batchContribution, batchGasUsed, nil
	}

	// If there are no regular transactions and there were also no atomic transactions to be included,
	// then the block is empty and should be considered invalid.
	if len(txs) == 0 {
		// this could happen due to the async logic of geth tx pool
		return nil, nil, nil, ErrEmptyBlock
	}

	// If there are no atomic transactions, but there is a non-zero number of regular transactions, then
	// we return a nil slice with no contribution from the atomic transactions and a nil error.
	return nil, nil, nil, nil
}

func (vm *VM) onFinalizeAndAssemble(
	header *types.Header,
	parent *types.Header,
	state *state.StateDB,
	txs []*types.Transaction,
) ([]byte, *big.Int, *big.Int, error) {
	if !vm.chainConfigExtra().IsApricotPhase5(header.Time) {
		return vm.preBatchOnFinalizeAndAssemble(header, state, txs)
	}
	return vm.postBatchOnFinalizeAndAssemble(header, parent, state, txs)
}

func (vm *VM) onExtraStateChange(block *types.Block, parent *types.Header, statedb *state.StateDB) (*big.Int, *big.Int, error) {
	var (
		batchContribution = big.NewInt(0)
		batchGasUsed      = big.NewInt(0)
		header            = block.Header()
		chainConfig       = vm.InnerVM.ChainConfig()
		// We cannot use chain config from InnerVM since it's not available when this function is called for the first time (bc.loadLastState).
		rules      = chainConfig.Rules(header.Number, params.IsMergeTODO, header.Time)
		rulesExtra = *params.GetRulesExtra(rules)
	)

	txs, err := atomic.ExtractAtomicTxs(customtypes.BlockExtData(block), rulesExtra.IsApricotPhase5, atomic.Codec)
	if err != nil {
		return nil, nil, err
	}

	// If [atomicBackend] is nil, the VM is still initializing and is reprocessing accepted blocks.
	if vm.AtomicBackend != nil {
		if vm.AtomicBackend.IsBonus(block.NumberU64(), block.Hash()) {
			log.Info("skipping atomic tx verification on bonus block", "block", block.Hash())
		} else {
			// Verify [txs] do not conflict with themselves or ancestor blocks.
			if err := vm.verifyTxs(txs, block.ParentHash(), block.BaseFee(), block.NumberU64(), rulesExtra); err != nil {
				return nil, nil, err
			}
		}
		// Update the atomic backend with [txs] from this block.
		//
		// Note: The atomic trie canonically contains the duplicate operations
		// from any bonus blocks.
		_, err := vm.AtomicBackend.InsertTxs(block.Hash(), block.NumberU64(), block.ParentHash(), txs)
		if err != nil {
			return nil, nil, err
		}
	}

	// If there are no transactions, we can return early.
	if len(txs) == 0 {
		return nil, nil, nil
	}

	wrappedStateDB := extstate.New(statedb)
	for _, tx := range txs {
		if err := tx.UnsignedAtomicTx.EVMStateTransfer(vm.Ctx, wrappedStateDB); err != nil {
			return nil, nil, err
		}
		// If ApricotPhase4 is enabled, calculate the block fee contribution
		if rulesExtra.IsApricotPhase4 {
			contribution, gasUsed, err := tx.BlockFeeContribution(rulesExtra.IsApricotPhase5, vm.Ctx.AVAXAssetID, block.BaseFee())
			if err != nil {
				return nil, nil, err
			}

			batchContribution.Add(batchContribution, contribution)
			batchGasUsed.Add(batchGasUsed, gasUsed)
		}
	}

	// If ApricotPhase5 is enabled, enforce that the atomic gas used does not exceed the
	// atomic gas limit.
	if rulesExtra.IsApricotPhase5 {
		chainConfigExtra := params.GetExtra(chainConfig)
		atomicGasLimit, err := customheader.RemainingAtomicGasCapacity(chainConfigExtra, parent, header)
		if err != nil {
			return nil, nil, err
		}

		if !utils.BigLessOrEqualUint64(batchGasUsed, atomicGasLimit) {
			return nil, nil, fmt.Errorf("%w: (%d) by block (%s), limit (%d)", errAtomicGasExceedsLimit, batchGasUsed, block.Hash().Hex(), atomicGasLimit)
		}
	}
	return batchContribution, batchGasUsed, nil
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return vm.BuildBlockWithContext(ctx, nil)
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, proposerVMBlockCtx *block.Context) (snowman.Block, error) {
	blk, err := vm.InnerVM.BuildBlockWithContext(ctx, proposerVMBlockCtx)

	// Handle errors and signal the mempool to take appropriate action
	switch {
	case err == nil:
		// Marks the current transactions from the mempool as being successfully issued
		// into a block.
		vm.AtomicMempool.IssueCurrentTxs()
	case errors.Is(err, vmerrors.ErrGenerateBlockFailed), errors.Is(err, vmerrors.ErrBlockVerificationFailed):
		log.Debug("cancelling txs due to error generating block", "err", err)
		vm.AtomicMempool.CancelCurrentTxs()
	case errors.Is(err, vmerrors.ErrWrapBlockFailed):
		log.Debug("discarding txs due to error making new block", "err", err)
		vm.AtomicMempool.DiscardCurrentTxs()
	}
	return blk, err
}

func (vm *VM) chainConfigExtra() *extras.ChainConfig {
	return params.GetExtra(vm.InnerVM.ChainConfig())
}

func (vm *VM) rules(number *big.Int, time uint64) extras.Rules {
	ethrules := vm.InnerVM.ChainConfig().Rules(number, params.IsMergeTODO, time)
	return *params.GetRulesExtra(ethrules)
}

// CurrentRules returns the chain rules for the current block.
func (vm *VM) CurrentRules() extras.Rules {
	header := vm.InnerVM.Ethereum().BlockChain().CurrentHeader()
	return vm.rules(header.Number, header.Time)
}

// TODO: these should be unexported after test refactor is done

// getAtomicTx returns the requested transaction, status, and height.
// If the status is Unknown, then the returned transaction will be nil.
func (vm *VM) GetAtomicTx(txID ids.ID) (*atomic.Tx, atomic.Status, uint64, error) {
	if tx, height, err := vm.AtomicTxRepository.GetByTxID(txID); err == nil {
		return tx, atomic.Accepted, height, nil
	} else if err != avalanchedatabase.ErrNotFound {
		return nil, atomic.Unknown, 0, err
	}
	tx, dropped, found := vm.AtomicMempool.GetTx(txID)
	switch {
	case found && dropped:
		return tx, atomic.Dropped, 0, nil
	case found:
		return tx, atomic.Processing, 0, nil
	default:
		return nil, atomic.Unknown, 0, nil
	}
}
