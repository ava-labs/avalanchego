// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var (
	_ Manager = (*manager)(nil)

	ErrChainNotSynced = errors.New("chain not synced")
)

type Manager interface {
	state.Versions

	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID

	SetPreference(blkID ids.ID) (updated bool)
	Preferred() ids.ID

	GetBlock(blkID ids.ID) (snowman.Block, error)
	GetStatelessBlock(blkID ids.ID) (block.Block, error)
	NewBlock(block.Block) snowman.Block

	// VerifyTx verifies that the transaction can be issued based on the currently
	// preferred state. This should *not* be used to verify transactions in a block.
	VerifyTx(tx *txs.Tx) (commonfees.TipPercentage, error)

	// VerifyUniqueInputs verifies that the inputs are not duplicated in the
	// provided blk or any of its ancestors pinned in memory.
	VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	s state.State,
	txExecutorBackend *executor.Backend,
	validatorManager validators.Manager,
) Manager {
	lastAccepted := s.GetLastAccepted()
	backend := &backend{
		Mempool:      mempool,
		lastAccepted: lastAccepted,
		preferred:    lastAccepted,
		state:        s,
		ctx:          txExecutorBackend.Ctx,
		blkIDToState: map[ids.ID]*blockState{},
	}

	return &manager{
		backend: backend,
		verifier: &verifier{
			backend:           backend,
			txExecutorBackend: txExecutorBackend,
		},
		acceptor: &acceptor{
			backend:      backend,
			metrics:      metrics,
			validators:   validatorManager,
			bootstrapped: txExecutorBackend.Bootstrapped,
		},
		rejector: &rejector{
			backend:           backend,
			txExecutorBackend: txExecutorBackend,
			addTxsToMempool:   !txExecutorBackend.Config.PartialSyncPrimaryNetwork,
		},
		txExecutorBackend: txExecutorBackend,
	}
}

type manager struct {
	*backend
	verifier block.Visitor
	acceptor block.Visitor
	rejector block.Visitor

	txExecutorBackend *executor.Backend
}

func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	blk, err := m.backend.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	return m.NewBlock(blk), nil
}

func (m *manager) GetStatelessBlock(blkID ids.ID) (block.Block, error) {
	return m.backend.GetBlock(blkID)
}

func (m *manager) NewBlock(blk block.Block) snowman.Block {
	return &Block{
		manager: m,
		Block:   blk,
	}
}

func (m *manager) VerifyTx(tx *txs.Tx) (commonfees.TipPercentage, error) {
	if !m.txExecutorBackend.Bootstrapped.Get() {
		return commonfees.NoTip, ErrChainNotSynced
	}

	stateDiff, err := state.NewDiff(m.Preferred(), m)
	if err != nil {
		return commonfees.NoTip, err
	}

	// retrieve parent block time before moving time forward
	parentBlkTime := stateDiff.GetTimestamp()

	nextBlkTime, _, err := state.NextBlockTime(stateDiff, m.txExecutorBackend.Clk)
	if err != nil {
		return commonfees.NoTip, err
	}

	_, err = executor.AdvanceTimeTo(m.txExecutorBackend, stateDiff, nextBlkTime)
	if err != nil {
		return commonfees.NoTip, err
	}

	feeRates, err := stateDiff.GetFeeRates()
	if err != nil {
		return commonfees.NoTip, err
	}

	var (
		isEActive = m.txExecutorBackend.Config.IsEActivated(nextBlkTime)
		feesCfg   = config.GetDynamicFeesConfig(isEActive)
	)

	feeManager := commonfees.NewManager(feeRates)
	if isEActive {
		feeManager, err = fee.UpdatedFeeManager(stateDiff, m.txExecutorBackend.Config, parentBlkTime, nextBlkTime)
		if err != nil {
			return commonfees.NoTip, err
		}
	}

	standardExecutor := &executor.StandardTxExecutor{
		Backend:            m.txExecutorBackend,
		BlkFeeManager:      feeManager,
		BlockMaxComplexity: feesCfg.BlockMaxComplexity,
		State:              stateDiff,
		Tx:                 tx,
	}
	err = tx.Unsigned.Visit(standardExecutor)
	return standardExecutor.TipPercentage, err
}

func (m *manager) VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error {
	return m.backend.verifyUniqueInputs(blkID, inputs)
}
