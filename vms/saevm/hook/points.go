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
	saestate "github.com/ava-labs/avalanchego/vms/saevm/state"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
	"github.com/ava-labs/avalanchego/x/blockdb"
)

var _ hook.PointsG[*txpool.Tx] = (*Points)(nil)

type Points struct {
	blockBuilder
	db database.Database
}

func NewPoints(
	ctx *snow.Context,
	consensusState *utils.Atomic[snow.State],
	pool *txpool.Txs,
	db database.Database,
) *Points {
	return &Points{
		blockBuilder{
			ctx:            ctx,
			consensusState: consensusState,
			potentialTxs:   pool.Iter,
		},
		db,
	}
}

func (p *Points) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*txpool.Tx], error) {
	rawTxs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("failed to extract txs of block %s (%d): %v", b.Hash(), b.NumberU64(), err)
	}

	txs := make([]*txpool.Tx, len(rawTxs))
	for i, rawTx := range rawTxs {
		tx, err := txpool.NewTx(rawTx, p.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tx %d for block %s (%d): %v", i, b.Hash(), b.NumberU64(), err)
		}
		txs[i] = tx
	}

	return &blockBuilder{
		ctx:            p.ctx,
		consensusState: p.consensusState,
		now: func() time.Time {
			return time.Unix(int64(b.Time()), 0)
		},
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

func (*Points) GasConfigAfter(*types.Header) (gas.Gas, hook.GasPriceConfig) {
	return 1_000_000, hook.GasPriceConfig{
		TargetToExcessScaling: 87,
		MinPrice:              1,
		StaticPricing:         false,
	}
}

func (*Points) SubSecondBlockTime(*types.Header) time.Duration {
	return 0
}

func (p *Points) EndOfBlockOps(b *types.Block) ([]hook.Op, error) {
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("failed to extract txs of block %s (%d): %v", b.Hash(), b.NumberU64(), err)
	}

	ops := make([]hook.Op, len(txs))
	for i, tx := range txs {
		op, err := tx.AsOp(p.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tx %d to op for block %s (%d): %v", i, b.Hash(), b.NumberU64(), err)
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
		return fmt.Errorf("failed to extract txs of block %s (%d): %v", b.Hash(), b.NumberU64(), err)
	}

	extstatedb := extstate.New(statedb)
	for i, tx := range txs {
		txID, err := tx.ID()
		if err != nil {
			return fmt.Errorf("problem getting transaction ID %d for block %s (%d): %w", i, b.Hash(), b.NumberU64(), err)
		}
		if err := tx.TransferNonAVAX(p.ctx.AVAXAssetID, extstatedb); err != nil {
			return fmt.Errorf("failed to transfer non-AVAX assets of tx %s in block %s (%d): %v", txID, b.Hash(), b.NumberU64(), err)
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
		return fmt.Errorf("failed to write txs of block %s (%d) to db: %v", b.Hash(), height, err)
	}

	ops, err := atomicOpsOf(txs)
	if err != nil {
		return fmt.Errorf("failed to extract atomic ops of block %s (%d): %v", b.Hash(), height, err)
	}

	// TODO: Add ops to the atomic trie.

	// // Insert the operations into the atomic trie
	// //
	// // Note: The atomic trie canonically contains the duplicate operations from
	// // any bonus blocks.
	// atomicOps, err := mergeAtomicOps(txs)
	// if err != nil {
	// 	return common.Hash{}, err
	// }
	// if err := a.atomicTrie.UpdateTrie(tr, blockHeight, atomicOps); err != nil {
	// 	return common.Hash{}, err
	// }

	// // If block hash is not provided, we do not pin the atomic state in memory and can return early
	// if blockHash == (common.Hash{}) {
	// 	return tr.Hash(), nil
	// }

	// // get the new root and pin the atomic trie changes in memory.
	// root, nodes, err := tr.Commit(false)
	// if err != nil {
	// 	return common.Hash{}, err
	// }
	// if err := a.atomicTrie.InsertTrie(nodes, root); err != nil {
	// 	return common.Hash{}, err
	// }
	// // track this block so further blocks can be inserted on top
	// // of this block
	// a.verifiedRoots[blockHash] = &atomicState{
	// 	backend:     a,
	// 	blockHash:   blockHash,
	// 	blockHeight: blockHeight,
	// 	txs:         txs,
	// 	atomicOps:   atomicOps,
	// 	atomicRoot:  root,
	// }
	// return root, nil

	/*
			// Accept writes the atomic operations to the database and
		// updates the last accepted block in the atomic backend.
		// It also commits the `commitBatch` to the shared memory.
		func (a *atomicState) Accept(commitBatch database.Batch) error {
			isBonus := a.backend.IsBonus(a.blockHeight, a.blockHash)
			// Update the atomic tx repository. Note it is necessary to invoke
			// the correct method taking bonus blocks into consideration.
			if isBonus {
				if err := a.backend.repo.WriteBonus(a.blockHeight, a.txs); err != nil {
					return err
				}
			} else {
				if err := a.backend.repo.Write(a.blockHeight, a.txs); err != nil {
					return err
				}
			}

			// Accept the root of this atomic trie (will be persisted if at a commit interval)
			if _, err := a.backend.atomicTrie.AcceptTrie(a.blockHeight, a.atomicRoot); err != nil {
				return err
			}
			// Update the last accepted block to this block and remove it from
			// the map tracking undecided blocks.
			a.backend.lastAcceptedHash = a.blockHash
			delete(a.backend.verifiedRoots, a.blockHash)

			// get changes from the atomic trie and repository in a batch
			// to be committed atomically with [commitBatch] and shared memory.
			atomicChangesBatch, err := a.backend.repo.db.CommitBatch()
			if err != nil {
				return fmt.Errorf("could not create commit batch in atomicState accept: %w", err)
			}

			// If this is a bonus block, write [commitBatch] without applying atomic ops
			// to shared memory.
			if isBonus {
				log.Info("skipping atomic tx acceptance on bonus block", "block", a.blockHash)
				return avalancheatomic.WriteAll(commitBatch, atomicChangesBatch)
			}

			// Otherwise, atomically commit pending changes in the version db with
			// atomic ops to shared memory.
			return a.backend.sharedMemory.Apply(a.atomicOps, commitBatch, atomicChangesBatch)
		}
	*/

	// If this is a bonus block, write [commitBatch] without applying atomic ops
	// to shared memory.
	if isBonus {
		p.ctx.Log.Info("skipping shared memory application on bonus block",
			zap.Stringer("block_hash", b.Hash()),
			zap.Uint64("block_height", height),
		)
		return nil
	}

	lastAppliedHeight, err := saestate.ReadLastAppliedHeight(p.db)
	if err != nil {
		return fmt.Errorf("failed to read last applied height from db: %v", err)
	}

	// SAE may re-execute blocks on startup. If the atomic ops were already
	// applied to shared memory, we MUST not re-apply them.
	if lastAppliedHeight >= height {
		p.ctx.Log.Info("skipping shared memory application on already applied block",
			zap.Stringer("block_hash", b.Hash()),
			zap.Uint64("block_height", height),
			zap.Uint64("last_applied_height", lastAppliedHeight),
		)
		return nil
	}

	batch := p.db.NewBatch()
	if err := saestate.WriteLastAppliedHeight(batch, height); err != nil {
		return fmt.Errorf("failed to write last applied height of block %s (%d) to db: %v", b.Hash(), height, err)
	}
	if err := p.ctx.SharedMemory.Apply(ops, batch); err != nil {
		return fmt.Errorf("failed to apply atomic ops of block %s (%d) to shared memory: %v", b.Hash(), height, err)
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
