// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"fmt"
	"iter"
	"slices"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
	"github.com/ava-labs/avalanchego/x/blockdb"

	saestate "github.com/ava-labs/avalanchego/vms/saevm/state"
)

var _ hook.PointsG[*txpool.Tx] = (*Points)(nil)

type Points struct {
	blockBuilder
	db database.Database
}

func NewPoints(
	ctx *snow.Context,
	consensusState *utils.Atomic[snow.State],
	desiredTargetExcess *acp176.TargetExcess,
	pool *txpool.Txs,
	db database.Database,
) *Points {
	return &Points{
		blockBuilder{
			ctx:                 ctx,
			consensusState:      consensusState,
			desiredTargetExcess: desiredTargetExcess,
			potentialTxs:        pool.Iter,
		},
		db,
	}
}

func (p *Points) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*txpool.Tx], error) {
	rawTxs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("failed to extract txs of block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}

	txs := make([]*txpool.Tx, len(rawTxs))
	for i, rawTx := range rawTxs {
		tx, err := txpool.NewTx(rawTx, p.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tx %d for block %s (%d): %w", i, b.Hash(), b.NumberU64(), err)
		}
		txs[i] = tx
	}

	te := targetExcess(b.Header())
	return &blockBuilder{
		ctx:            p.ctx,
		consensusState: p.consensusState,
		now: func() time.Time {
			return time.Unix(int64(b.Time()), 0)
		},
		desiredTargetExcess: &te,
		potentialTxs: func() iter.Seq[*txpool.Tx] {
			return slices.Values(txs)
		},
	}, nil
}

func (p *Points) ExecutionResultsDB(dataDir string) (saedb.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		p.ctx.Log,
	)
	return saedb.ExecutionResults{HeightIndex: db}, err
}

func (*Points) GasConfigAfter(h *types.Header) (gas.Gas, hook.GasPriceConfig) {
	return targetExcess(h).Target(), hook.GasPriceConfig{
		TargetToExcessScaling: acp176.TargetToExcessScaling,
		MinPrice:              acp176.MinPrice,
	}
}

func targetExcess(h *types.Header) acp176.TargetExcess {
	if te := customtypes.GetHeaderExtra(h).TargetExcess; te != nil {
		return *te
	}
	return 0
}

func (*Points) SubSecondBlockTime(*types.Header) time.Duration {
	return 0
}

func (p *Points) EndOfBlockOps(b *types.Block) ([]hook.Op, error) {
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("failed to extract txs of block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}

	ops := make([]hook.Op, len(txs))
	for i, tx := range txs {
		op, err := tx.AsOp(p.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tx %d to op for block %s (%d): %w", i, b.Hash(), b.NumberU64(), err)
		}
		ops[i] = op
	}
	return ops, nil
}

func (*Points) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*Points) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (p *Points) AfterExecutingBlock(statedb *state.StateDB, b *types.Block, _ types.Receipts) error {
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return fmt.Errorf("failed to extract txs of block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}

	extstatedb := extstate.New(statedb)
	for i, tx := range txs {
		txID, err := tx.ID()
		if err != nil {
			return fmt.Errorf("problem getting transaction ID %d for block %s (%d): %w", i, b.Hash(), b.NumberU64(), err)
		}
		if err := tx.TransferNonAVAX(p.ctx.AVAXAssetID, extstatedb); err != nil {
			return fmt.Errorf("failed to transfer non-AVAX assets of tx %s in block %s (%d): %w", txID, b.Hash(), b.NumberU64(), err)
		}
	}

	height := b.NumberU64()
	bonusBlocks := saestate.BonusBlocks(p.ctx.NetworkID)
	isBonus := bonusBlocks.Contains(height)
	writeTxs := saestate.WriteTxs
	if isBonus {
		writeTxs = saestate.WriteBonusTxs
	}
	if err := writeTxs(p.db, height, txs); err != nil {
		return fmt.Errorf("failed to write txs of block %s (%d) to db: %w", b.Hash(), height, err)
	}

	ops, err := atomicOpsOf(txs)
	if err != nil {
		return fmt.Errorf("failed to extract atomic ops of block %s (%d): %w", b.Hash(), height, err)
	}

	/*

		var previousRoot common.Hash
		trieDB := saestate.NewTrieDB(p.db)
		tr, err := trie.New(trie.TrieID(previousRoot), trieDB)
		if err != nil {
			return fmt.Errorf("failed to create new trie: %v", err)
		}

		for chainID, requests := range ops {
			requestBytes, err := tx.MarshalAtomicRequests(requests)
			if err != nil {
				return fmt.Errorf("failed to marshal atomic requests for chain %s: %v", chainID, err)
			}

			// key is [height]+[blockchainID]
			const keyLength = wrappers.LongLen + ids.IDLen
			p := wrappers.Packer{Bytes: make([]byte, keyLength)}
			p.PackLong(height)
			p.PackFixedBytes(chainID[:])
			if err := tr.Update(p.Bytes, requestBytes); err != nil {
				return err
			}
		}

		root, nodes, err := tr.Commit(false)
		if err != nil {
			return err
		}
		if nodes != nil {
			if err := trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
				return err
			}
		}
		if err := trieDB.Reference(root, common.Hash{}); err != nil {
			return err
		}

		// Avoid OOM in-case there are a ton of atomic ops issued between DB
		// commits.
		const (
			capTrigger = 64 * units.MiB
			capLimit   = capTrigger - ethdb.IdealBatchSize
		)
		if _, nodeSize, _ := trieDB.Size(); nodeSize > capTrigger {
			if err := trieDB.Cap(capLimit); err != nil {
				return fmt.Errorf("failed to cap atomic trie for root %s: %w", root, err)
			}
		}

		batch := p.db.NewBatch()

		const commitInterval = 4096
		if height%commitInterval == 0 {
			if err := trieDB.Commit(root, false); err != nil {
				return err
			}
			if err := saestate.WriteCommittedRoot(batch, height, root); err != nil {
				return err
			}
		}

		// The following dereferences, if any, the previously inserted root.
		// This one can be dereferenced whether it has been:
		// - committed, in which case the dereference is a no-op
		// - not committed, in which case the current root we are inserting contains
		//   references to all the relevant data from the previous root.
		if err := trieDB.Dereference(previousRoot); err != nil {
			return err
		}
	*/

	batch := p.db.NewBatch()

	// If this is a bonus block, write [commitBatch] without applying atomic ops
	// to shared memory.
	if isBonus {
		p.ctx.Log.Info("skipping shared memory application on bonus block",
			zap.Stringer("block_hash", b.Hash()),
			zap.Uint64("block_height", height),
		)
		return batch.Write()
	}

	lastAppliedHeight, err := saestate.ReadLastAppliedHeight(p.db)
	if err != nil {
		return fmt.Errorf("failed to read last applied height from db: %w", err)
	}

	// SAE may re-execute blocks on startup. If the atomic ops were already
	// applied to shared memory, we MUST not re-apply them.
	if lastAppliedHeight >= height {
		p.ctx.Log.Info("skipping shared memory application on already applied block",
			zap.Stringer("block_hash", b.Hash()),
			zap.Uint64("block_height", height),
			zap.Uint64("last_applied_height", lastAppliedHeight),
		)
		return batch.Write()
	}

	if err := saestate.WriteLastAppliedHeight(batch, height); err != nil {
		return fmt.Errorf("failed to write last applied height of block %s (%d) to db: %w", b.Hash(), height, err)
	}
	if err := p.ctx.SharedMemory.Apply(ops, batch); err != nil {
		return fmt.Errorf("failed to apply atomic ops of block %s (%d) to shared memory: %w", b.Hash(), height, err)
	}
	return nil
}

// atomicOpsOf returns the union of all atomic requests contained in txs.
func atomicOpsOf(txs []*tx.Tx) (map[ids.ID]*atomic.Requests, error) {
	// requests are appended in order of txID to be consistent with coreth's
	// historical atomic trie format.
	txs = slices.Clone(txs)
	utils.Sort(txs)

	ops := make(map[ids.ID]*atomic.Requests)
	for _, tx := range txs {
		txID, err := tx.ID()
		if err != nil {
			return nil, err
		}

		chainID, requests, err := tx.AtomicOps(txID)
		if err != nil {
			return nil, err
		}

		if request, ok := ops[chainID]; ok {
			request.PutRequests = append(request.PutRequests, requests.PutRequests...)
			request.RemoveRequests = append(request.RemoveRequests, requests.RemoveRequests...)
		} else {
			ops[chainID] = requests
		}
	}
	return ops, nil
}
