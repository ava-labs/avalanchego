// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"errors"
	"fmt"
	"iter"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/x/blockdb"
)

var _ hook.PointsG[*txpool.Transaction] = (*hooks)(nil)

type hooks struct {
	blockBuilder
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*txpool.Transaction], error) {
	return &blockBuilder{
		log: h.log,
		now: func() time.Time {
			return time.Unix(int64(b.Time()), 0)
		},
		potentialTxs: txpool.NewTxs(), // TODO: FIXME
	}, nil
}

func (h *hooks) ExecutionResultsDB(dataDir string) (saedb.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		h.log,
	)
	return saedb.ExecutionResults{HeightIndex: db}, err
}

func (*hooks) GasConfigAfter(*types.Header) (gas.Gas, hook.GasPriceConfig) {
	return 1_000_000, hook.GasPriceConfig{
		TargetToExcessScaling: 87,
		MinPrice:              1,
		StaticPricing:         false,
	}
}

func (*hooks) SubSecondBlockTime(*types.Header) time.Duration {
	return 0
}

func (*hooks) EndOfBlockOps(*types.Block) ([]hook.Op, error) {
	return nil, nil
}

func (*hooks) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*hooks) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (*hooks) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) {}

var _ hook.BlockBuilder[*txpool.Transaction] = (*blockBuilder)(nil)

type blockBuilder struct {
	log logging.Logger

	now          func() time.Time
	potentialTxs *txpool.Txs
}

func (b *blockBuilder) BuildHeader(parent *types.Header) *types.Header {
	var now time.Time
	if b.now != nil {
		now = b.now()
	} else {
		now = time.Now()
	}
	return &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		Time:       uint64(now.Unix()),
	}
}

func ancestorUTXOIDs(header *types.Header, settledHash common.Hash, getBlock blocks.EthBlockSource) (set.Set[ids.ID], error) {
	var consumedUTXOs set.Set[ids.ID]
	for header.ParentHash != settledHash {
		blockNumber := header.Number.Uint64() - 1
		block, ok := getBlock(header.ParentHash, blockNumber)
		if !ok {
			return nil, fmt.Errorf("missing block %s (%d)", header.ParentHash, blockNumber)
		}

		txs, err := atomic.ExtractAtomicTxs(
			customtypes.BlockExtData(block),
			true, // batch
			atomic.Codec,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to extract atomic txs of block %s (%d): %v", block.Hash(), block.NumberU64(), err)
		}

		for _, tx := range txs {
			consumedUTXOs.Union(tx.InputUTXOs())
		}
		header = block.Header()
	}
	return consumedUTXOs, nil
}

func emptyIter(yield func(*txpool.Transaction) bool) {}

func (b *blockBuilder) PotentialEndOfBlockOps() iter.Seq[*txpool.Transaction] {
	var (
		header      *types.Header
		settledHash common.Hash
		getBlock    blocks.EthBlockSource
	)
	consumedUTXOs, err := ancestorUTXOIDs(header, settledHash, getBlock)
	if err != nil {
		b.log.Error("failed to get ancestor UTXO IDs",
			zap.Error(err),
		)
		return emptyIter
	}

	return func(yield func(*txpool.Transaction) bool) {
		for tx := range b.potentialTxs.Iter() {
			if consumedUTXOs.Overlaps(tx.Inputs) {
				continue
			}

			if err := tx.Tx.Verify(&snow.Context{}, extras.Rules{}); err != nil {
				return err
			}

			if !yield(tx) {
				return
			}

			consumedUTXOs.Union(tx.Inputs)
		}
	}
}

var errEmptyBlock = errors.New("empty block")

func (*blockBuilder) BuildBlock(
	header *types.Header,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	atomicTxs []*txpool.Transaction,
) (*types.Block, error) {
	if len(txs) == 0 && len(atomicTxs) == 0 {
		return nil, errEmptyBlock
	}
	// TODO: Include atomic txs
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil)), nil
}

type txVerifier struct {
	ctx         *snow.Context
	chainConfig *params.ChainConfig
}

func (v *txVerifier) VerifyTx(h *types.Header, tx *atomic.Tx) error {
	ethRules := v.chainConfig.Rules(h.Number, true, h.Time)
	avaRules := *params.GetRulesExtra(ethRules)
	if err := tx.Verify(v.ctx, avaRules); err != nil {
		return err
	}
	return nil
}

func verifyFlowCheck(tx *atomic.Tx) error {
	fc := avax.NewFlowChecker()

	for _, out := range tx.ExportedOutputs {
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	for _, in := range tx.Ins {
		fc.Consume(in.AssetID, in.Amount)
	}

	if err := fc.Verify(); err != nil {
		return fmt.Errorf("flow check failed: %v", err)
	}
	return nil
}

// ImportTx verifies this transaction is valid.
func (s *semanticVerifier) ImportTx(utx *atomic.UnsignedImportTx) error {
	backend := s.backend
	ctx := backend.Ctx
	rules := backend.Rules
	stx := s.tx
	if err := utx.Verify(ctx, rules); err != nil {
		return err
	}

	// Check the transaction consumes and produces the right amounts
	fc := avax.NewFlowChecker()
	for _, out := range utx.Outs {
		fc.Produce(out.AssetID, out.Amount)
	}
	for _, in := range utx.ImportedInputs {
		fc.Consume(in.AssetID(), in.Input().Amount())
	}
	if err := fc.Verify(); err != nil {
		return fmt.Errorf("flow check failed: %v", err)
	}

	if len(stx.Creds) != len(utx.ImportedInputs) {
		return fmt.Errorf("%w: (%d vs. %d)", errIncorrectNumCredentials, len(utx.ImportedInputs), len(stx.Creds))
	}

	if !backend.Bootstrapped {
		// Allow for force committing during bootstrapping
		return nil
	}

	utxoIDs := make([][]byte, len(utx.ImportedInputs))
	for i, in := range utx.ImportedInputs {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}
	// allUTXOBytes is guaranteed to be the same length as utxoIDs
	allUTXOBytes, err := ctx.SharedMemory.Get(utx.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("%w from %s due to: %w", errFailedToFetchImportUTXOs, utx.SourceChain, err)
	}

	for i, in := range utx.ImportedInputs {
		utxoBytes := allUTXOBytes[i]

		utxo := &avax.UTXO{}
		if _, err := atomic.Codec.Unmarshal(utxoBytes, utxo); err != nil {
			return fmt.Errorf("%w: %w", errFailedToUnmarshalUTXO, err)
		}

		cred := stx.Creds[i]

		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if utxoAssetID != inAssetID {
			return ErrAssetIDMismatch
		}

		if err := backend.Fx.VerifyTransfer(utx, in.In, cred, utxo.Out); err != nil {
			return fmt.Errorf("import tx transfer failed verification: %w", err)
		}
	}

	return conflicts(backend, utx.InputUTXOs(), s.parent)
}

// ExportTx verifies this transaction is valid.
func (s *semanticVerifier) ExportTx(utx *atomic.UnsignedExportTx) error {
	backend := s.backend
	ctx := backend.Ctx
	rules := backend.Rules
	stx := s.tx
	if err := utx.Verify(ctx, rules); err != nil {
		return err
	}

	// Check the transaction consumes and produces the right amounts
	fc := avax.NewFlowChecker()
	switch {
	// Apply dynamic fees to export transactions as of Apricot Phase 3
	case rules.IsApricotPhase3:
		gasUsed, err := stx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return err
		}
		txFee, err := atomic.CalculateDynamicFee(gasUsed, s.baseFee)
		if err != nil {
			return err
		}
		fc.Produce(ctx.AVAXAssetID, txFee)
	// Apply fees to export transactions before Apricot Phase 3
	default:
		fc.Produce(ctx.AVAXAssetID, ap0.AtomicTxFee)
	}
	for _, out := range utx.ExportedOutputs {
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	for _, in := range utx.Ins {
		fc.Consume(in.AssetID, in.Amount)
	}

	if err := fc.Verify(); err != nil {
		return fmt.Errorf("export tx flow check failed due to: %w", err)
	}

	if len(utx.Ins) != len(stx.Creds) {
		return fmt.Errorf("export tx contained %w want %d got %d", errIncorrectNumCredentials, len(utx.Ins), len(stx.Creds))
	}

	for i, input := range utx.Ins {
		cred, ok := stx.Creds[i].(*secp256k1fx.Credential)
		if !ok {
			return fmt.Errorf("expected *secp256k1fx.Credential but got %T", cred)
		}
		if err := cred.Verify(); err != nil {
			return err
		}

		if len(cred.Sigs) != 1 {
			return fmt.Errorf("%w want 1 signature for EVM Input Credential, but got %d", errIncorrectNumSignatures, len(cred.Sigs))
		}
		pubKey, err := s.backend.SecpCache.RecoverPublicKey(utx.Bytes(), cred.Sigs[0][:])
		if err != nil {
			return err
		}
		if input.Address != pubKey.EthAddress() {
			return errPublicKeySignatureMismatch
		}
	}

	return nil
}
