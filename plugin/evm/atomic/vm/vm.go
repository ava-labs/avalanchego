// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/atomic/sync"
	"github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	customheader "github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/utils"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
)

var (
	_ secp256k1fx.VM                     = (*VM)(nil)
	_ block.ChainVM                      = (*VM)(nil)
	_ block.BuildBlockWithContextChainVM = (*VM)(nil)
	_ block.StateSyncableVM              = (*VM)(nil)
)

var (
	secpCacheSize       = 1024
	targetAtomicTxsSize = 40 * units.KiB
)

// TODO: remove this
// InnerVM is the interface that must be implemented by the VM
// that's being wrapped by the extension
type InnerVM interface {
	extension.ExtensibleVM
	avalanchecommon.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM

	// TODO: remove these
	AtomicMempool() *txpool.Mempool
}

type VM struct {
	InnerVM
	ctx *snow.Context

	// TODO: unexport these fields
	SecpCache *secp256k1.RecoverCache
	Fx        secp256k1fx.Fx
	baseCodec codec.Registry

	// [atomicTxRepository] maintains two indexes on accepted atomic txs.
	// - txID to accepted atomic tx
	// - block height to list of atomic txs accepted on block at that height
	// TODO: unexport these fields
	AtomicTxRepository *atomicstate.AtomicRepository
	// [atomicBackend] abstracts verification and processing of atomic transactions
	AtomicBackend *atomicstate.AtomicBackend

	clock mockable.Clock
}

func WrapVM(vm InnerVM) *VM {
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
	toEngine chan<- avalanchecommon.Message,
	fxs []*avalanchecommon.Fx,
	appSender avalanchecommon.AppSender,
) error {
	vm.ctx = chainCtx

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
	syncExtender := &sync.Extender{}
	syncProvider := &sync.SummaryProvider{}
	// Create and pass the leaf handler to the atomic extension
	// it will be initialized after the inner VM is initialized
	leafHandler := sync.NewLeafHandler()
	atomicLeafTypeConfig := &extension.LeafRequestConfig{
		LeafType:   sync.TrieNode,
		MetricName: "sync_atomic_trie_leaves",
		Handler:    leafHandler,
	}

	extensionConfig := &extension.Config{
		ConsensusCallbacks:         vm.createConsensusCallbacks(),
		BlockExtender:              blockExtender,
		SyncableParser:             sync.NewSummaryParser(),
		SyncExtender:               syncExtender,
		SyncSummaryProvider:        syncProvider,
		ExtraSyncLeafHandlerConfig: atomicLeafTypeConfig,
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
		toEngine,
		fxs,
		appSender,
	); err != nil {
		return fmt.Errorf("failed to initialize inner VM: %w", err)
	}

	// initialize bonus blocks on mainnet
	var (
		bonusBlockHeights map[uint64]ids.ID
	)
	if vm.ctx.NetworkID == constants.MainnetID {
		var err error
		bonusBlockHeights, err = readMainnetBonusBlocks()
		if err != nil {
			return fmt.Errorf("failed to read mainnet bonus blocks: %w", err)
		}
	}

	// initialize atomic repository
	lastAcceptedHash, lastAcceptedHeight, err := vm.ReadLastAccepted()
	if err != nil {
		return fmt.Errorf("failed to read last accepted block: %w", err)
	}
	vm.AtomicTxRepository, err = atomicstate.NewAtomicTxRepository(vm.VersionDB(), atomic.Codec, lastAcceptedHeight)
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}
	vm.AtomicBackend, err = atomicstate.NewAtomicBackend(
		vm.ctx.SharedMemory, bonusBlockHeights,
		vm.AtomicTxRepository, lastAcceptedHeight, lastAcceptedHash,
		vm.Config().CommitInterval,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic backend: %w", err)
	}

	// Atomic backend is available now, we can initialize structs that depend on it
	atomicTrie := vm.AtomicBackend.AtomicTrie()
	syncProvider.Initialize(atomicTrie)
	syncExtender.Initialize(vm.AtomicBackend, atomicTrie, vm.Config().StateSyncRequestSize)
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
	case snow.Bootstrapping:
		if err := vm.onBootstrapStarted(); err != nil {
			return err
		}
	case snow.NormalOp:
		if err := vm.onNormalOperationsStarted(); err != nil {
			return err
		}
	}

	// TODO: uncomment this once atomic VM fully wraps inner VM
	// return vm.InnerVM.SetState(ctx, state)
	return nil
}

func (vm *VM) onBootstrapStarted() error {
	return vm.Fx.Bootstrapping()
}

func (vm *VM) onNormalOperationsStarted() error {
	if vm.IsBootstrapped() {
		return nil
	}
	return vm.Fx.Bootstrapped()
}

// VerifyTx verifies that [tx] is valid to be issued into a block with parent block [parentHash]
// and validated at [state] using [rules] as the current rule set.
// Note: VerifyTx may modify [state]. If [state] needs to be properly maintained, the caller is responsible
// for reverting to the correct snapshot after calling this function. If this function is called with a
// throwaway state, then this is not necessary.
// TODO: unexport this function
func (vm *VM) VerifyTx(tx *atomic.Tx, parentHash common.Hash, baseFee *big.Int, state *state.StateDB, rules extras.Rules) error {
	parent, err := vm.InnerVM.GetExtendedBlock(context.TODO(), ids.ID(parentHash))
	if err != nil {
		return fmt.Errorf("failed to get parent block: %w", err)
	}
	atomicBackend := &VerifierBackend{
		Ctx:          vm.ctx,
		Fx:           &vm.Fx,
		Rules:        rules,
		Bootstrapped: vm.IsBootstrapped(),
		BlockFetcher: vm,
		SecpCache:    vm.SecpCache,
	}
	if err := tx.UnsignedAtomicTx.Visit(&SemanticVerifier{
		Backend: atomicBackend,
		Tx:      tx,
		Parent:  parent,
		BaseFee: baseFee,
	}); err != nil {
		return err
	}
	return tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state)
}

// verifyTxs verifies that [txs] are valid to be issued into a block with parent block [parentHash]
// using [rules] as the current rule set.
func (vm *VM) verifyTxs(txs []*atomic.Tx, parentHash common.Hash, baseFee *big.Int, height uint64, rules extras.Rules) error {
	// Ensure that the parent was verified and inserted correctly.
	if !vm.Ethereum().BlockChain().HasBlock(parentHash, height-1) {
		return errRejectedParent
	}

	ancestorID := ids.ID(parentHash)
	// If the ancestor is unknown, then the parent failed verification when
	// it was called.
	// If the ancestor is rejected, then this block shouldn't be inserted
	// into the canonical chain because the parent will be missing.
	ancestor, err := vm.GetExtendedBlock(context.TODO(), ancestorID)
	if err != nil {
		return errRejectedParent
	}

	// Ensure each tx in [txs] doesn't conflict with any other atomic tx in
	// a processing ancestor block.
	inputs := set.Set[ids.ID]{}
	atomicBackend := &VerifierBackend{
		Ctx:          vm.ctx,
		Fx:           &vm.Fx,
		Rules:        rules,
		Bootstrapped: vm.IsBootstrapped(),
		BlockFetcher: vm,
		SecpCache:    vm.SecpCache,
	}

	for _, atomicTx := range txs {
		utx := atomicTx.UnsignedAtomicTx
		if err := utx.Visit(&SemanticVerifier{
			Backend: atomicBackend,
			Tx:      atomicTx,
			Parent:  ancestor,
			BaseFee: baseFee,
		}); err != nil {
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
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

func (vm *VM) createConsensusCallbacks() dummy.ConsensusCallbacks {
	return dummy.ConsensusCallbacks{
		OnFinalizeAndAssemble: vm.onFinalizeAndAssemble,
		OnExtraStateChange:    vm.onExtraStateChange,
	}
}

func (vm *VM) preBatchOnFinalizeAndAssemble(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	for {
		tx, exists := vm.AtomicMempool().NextTx()
		if !exists {
			break
		}
		// Take a snapshot of [state] before calling verifyTx so that if the transaction fails verification
		// we can revert to [snapshot].
		// Note: snapshot is taken inside the loop because you cannot revert to the same snapshot more than
		// once.
		snapshot := state.Snapshot()
		rules := vm.rules(header.Number, header.Time)
		if err := vm.VerifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx from mempool on failed verification", "txID", tx.ID(), "err", err)
			vm.AtomicMempool().DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, tx)
		if err != nil {
			// Discard the transaction from the mempool and error if the transaction
			// cannot be marshalled. This should never happen.
			log.Debug("discarding tx due to unmarshal err", "txID", tx.ID(), "err", err)
			vm.AtomicMempool().DiscardCurrentTx(tx.ID())
			return nil, nil, nil, fmt.Errorf("failed to marshal atomic transaction %s due to %w", tx.ID(), err)
		}
		var contribution, gasUsed *big.Int
		if rules.IsApricotPhase4 {
			contribution, gasUsed, err = tx.BlockFeeContribution(rules.IsApricotPhase5, vm.ctx.AVAXAssetID, header.BaseFee)
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
		batchContribution *big.Int = new(big.Int).Set(common.Big0)
		batchGasUsed      *big.Int = new(big.Int).Set(common.Big0)
		rules                      = vm.rules(header.Number, header.Time)
		size              int
	)

	atomicGasLimit, err := customheader.RemainingAtomicGasCapacity(vm.chainConfigExtra(), parent, header)
	if err != nil {
		return nil, nil, nil, err
	}

	for {
		tx, exists := vm.AtomicMempool().NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size soft limit.
		txSize := len(tx.SignedBytes())
		if size+txSize > targetAtomicTxsSize {
			vm.AtomicMempool().CancelCurrentTx(tx.ID())
			break
		}

		var (
			txGasUsed, txContribution *big.Int
			err                       error
		)

		// Note: we do not need to check if we are in at least ApricotPhase4 here because
		// we assume that this function will only be called when the block is in at least
		// ApricotPhase5.
		txContribution, txGasUsed, err = tx.BlockFeeContribution(true, vm.ctx.AVAXAssetID, header.BaseFee)
		if err != nil {
			return nil, nil, nil, err
		}
		// ensure `gasUsed + batchGasUsed` doesn't exceed `atomicGasLimit`
		if totalGasUsed := new(big.Int).Add(batchGasUsed, txGasUsed); !utils.BigLessOrEqualUint64(totalGasUsed, atomicGasLimit) {
			// Send [tx] back to the mempool's tx heap.
			vm.AtomicMempool().CancelCurrentTx(tx.ID())
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
			vm.AtomicMempool().DiscardCurrentTx(tx.ID())
			continue
		}

		snapshot := state.Snapshot()
		if err := vm.VerifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool and reset the state to [snapshot]
			// if it fails verification here.
			// Note: prior to this point, we have not modified [state] so there is no need to
			// revert to a snapshot if we discard the transaction prior to this point.
			log.Debug("discarding tx from mempool due to failed verification", "txID", tx.ID(), "err", err)
			vm.AtomicMempool().DiscardCurrentTx(tx.ID())
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
			vm.AtomicMempool().DiscardCurrentTxs()
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

func (vm *VM) onExtraStateChange(block *types.Block, parent *types.Header, state *state.StateDB) (*big.Int, *big.Int, error) {
	var (
		batchContribution *big.Int = big.NewInt(0)
		batchGasUsed      *big.Int = big.NewInt(0)
		header                     = block.Header()
		chainConfig                = vm.ChainConfig()
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

	for _, tx := range txs {
		if err := tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state); err != nil {
			return nil, nil, err
		}
		// If ApricotPhase4 is enabled, calculate the block fee contribution
		if rulesExtra.IsApricotPhase4 {
			contribution, gasUsed, err := tx.BlockFeeContribution(rulesExtra.IsApricotPhase5, vm.ctx.AVAXAssetID, block.BaseFee())
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
			return nil, nil, fmt.Errorf("atomic gas used (%d) by block (%s), exceeds atomic gas limit (%d)", batchGasUsed, block.Hash().Hex(), atomicGasLimit)
		}
	}
	return batchContribution, batchGasUsed, nil
}

func (vm *VM) chainConfigExtra() *extras.ChainConfig {
	return params.GetExtra(vm.InnerVM.Ethereum().BlockChain().Config())
}

func (vm *VM) rules(number *big.Int, time uint64) extras.Rules {
	ethrules := vm.ChainConfig().Rules(number, params.IsMergeTODO, time)
	return *params.GetRulesExtra(ethrules)
}
