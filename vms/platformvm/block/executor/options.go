// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ block.Visitor = (*verifier)(nil)

// options supports build new option blocks
type options struct {
	// inputs populated before calling this struct's methods:
	primaryUptimePercentage float64
	uptimes                 uptime.Calculator
	state                   state.Chain

	// outputs populated by this struct's methods:
	preferredBlock block.Block
	alternateBlock block.Block
}

func (*options) BanffAbortBlock(*block.BanffAbortBlock) error {
	return snowman.ErrNotOracle
}

func (*options) BanffCommitBlock(*block.BanffCommitBlock) error {
	return snowman.ErrNotOracle
}

func (o *options) BanffProposalBlock(b *block.BanffProposalBlock) error {
	timestamp := b.Timestamp()
	blkID := b.ID()
	nextHeight := b.Height() + 1

	commitBlock, err := block.NewBanffCommitBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}

	abortBlock, err := block.NewBanffAbortBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}

	if o.prefersCommit(b.Tx) {
		o.preferredBlock = commitBlock
		o.alternateBlock = abortBlock
	} else {
		o.preferredBlock = abortBlock
		o.alternateBlock = commitBlock
	}
	return nil
}

func (*options) BanffStandardBlock(*block.BanffStandardBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotAbortBlock(*block.ApricotAbortBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotCommitBlock(*block.ApricotCommitBlock) error {
	return snowman.ErrNotOracle
}

func (o *options) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	blkID := b.ID()
	nextHeight := b.Height() + 1

	var err error
	o.preferredBlock, err = block.NewApricotCommitBlock(blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}

	o.alternateBlock, err = block.NewApricotAbortBlock(blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}
	return nil
}

func (*options) ApricotStandardBlock(*block.ApricotStandardBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotAtomicBlock(*block.ApricotAtomicBlock) error {
	return snowman.ErrNotOracle
}

// TODO: should prefersCommit log errors
func (o *options) prefersCommit(tx *txs.Tx) bool {
	unsignedTx, ok := tx.Unsigned.(*txs.RewardValidatorTx)
	if !ok {
		return true
	}

	stakerTx, _, err := o.state.GetTx(unsignedTx.TxID)
	if err != nil {
		return true
	}

	staker, ok := stakerTx.Unsigned.(txs.Staker)
	if !ok {
		return true
	}

	nodeID := staker.NodeID()
	primaryNetworkValidator, err := o.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		nodeID,
	)
	if err != nil {
		return true
	}

	expectedUptimePercentage := o.primaryUptimePercentage
	if subnetID := staker.SubnetID(); subnetID != constants.PrimaryNetworkID {
		transformSubnet, err := executor.GetTransformSubnetTx(o.state, subnetID)
		if err != nil {
			return true
		}

		expectedUptimePercentage = float64(transformSubnet.UptimeRequirement) / reward.PercentDenominator
	}

	// TODO: calculate subnet uptimes
	uptime, err := o.uptimes.CalculateUptimePercentFrom(
		nodeID,
		constants.PrimaryNetworkID,
		primaryNetworkValidator.StartTime,
	)
	if err != nil {
		return true
	}

	return uptime >= expectedUptimePercentage
}
