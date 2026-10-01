// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_ platform.BlockVisitor = (*options)(nil)

	errUnexpectedProposalTxType           = errors.New("unexpected proposal transaction type")
	errFailedFetchingStakerTx             = errors.New("failed fetching staker transaction")
	errUnexpectedStakerTxType             = errors.New("unexpected staker transaction type")
	errFailedFetchingPrimaryStaker        = errors.New("failed fetching primary staker")
	errFailedFetchingSubnetTransformation = errors.New("failed fetching subnet transformation")
	errFailedCalculatingUptime            = errors.New("failed calculating uptime")
)

// options supports build new option blocks
type options struct {
	// inputs populated before calling this struct's methods:
	log                     logging.Logger
	primaryUptimePercentage float64
	upgradeConfig           upgrade.Config
	uptimes                 uptime.Calculator
	state                   state.Chain

	// outputs populated by this struct's methods:
	preferredBlock platform.Block
	alternateBlock platform.Block
}

func (*options) BanffAbortBlock(*platform.BanffAbortBlock) error {
	return snowman.ErrNotOracle
}

func (*options) BanffCommitBlock(*platform.BanffCommitBlock) error {
	return snowman.ErrNotOracle
}

func (o *options) BanffProposalBlock(b *platform.BanffProposalBlock) error {
	timestamp := b.Timestamp()
	blkID := b.ID()
	nextHeight := b.Height() + 1

	commitBlock, err := platform.NewBanffCommitBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}

	abortBlock, err := platform.NewBanffAbortBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}

	prefersCommit, err := o.prefersCommit(b.Tx)
	if err != nil {
		o.log.Debug("falling back to prefer commit",
			zap.Error(err),
		)
		// We fall back to commit here to err on the side of over-rewarding
		// rather than under-rewarding.
		//
		// Invariant: We must not return the error here, because the error would
		// be treated as fatal. Errors can occur here due to a malicious block
		// proposer or even in unusual virtuous cases.
		prefersCommit = true
	}

	if prefersCommit {
		o.preferredBlock = commitBlock
		o.alternateBlock = abortBlock
	} else {
		o.preferredBlock = abortBlock
		o.alternateBlock = commitBlock
	}
	return nil
}

func (*options) BanffStandardBlock(*platform.BanffStandardBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotAbortBlock(*platform.ApricotAbortBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotCommitBlock(*platform.ApricotCommitBlock) error {
	return snowman.ErrNotOracle
}

func (o *options) ApricotProposalBlock(b *platform.ApricotProposalBlock) error {
	blkID := b.ID()
	nextHeight := b.Height() + 1

	var err error
	o.preferredBlock, err = platform.NewApricotCommitBlock(blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}

	o.alternateBlock, err = platform.NewApricotAbortBlock(blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}
	return nil
}

func (*options) ApricotStandardBlock(*platform.ApricotStandardBlock) error {
	return snowman.ErrNotOracle
}

func (*options) ApricotAtomicBlock(*platform.ApricotAtomicBlock) error {
	return snowman.ErrNotOracle
}

func (o *options) prefersCommit(tx *platform.Tx) (bool, error) {
	unsignedTx, ok := tx.Unsigned.(platform.RewardTx)
	if !ok {
		return false, fmt.Errorf("%w: %T", errUnexpectedProposalTxType, tx.Unsigned)
	}

	stakerTx, _, err := o.state.GetTx(unsignedTx.StakerTxID())
	if err != nil {
		return false, fmt.Errorf("%w: %w", errFailedFetchingStakerTx, err)
	}

	staker, ok := stakerTx.Unsigned.(platform.Staker)
	if !ok {
		return false, fmt.Errorf("%w: %T", errUnexpectedStakerTxType, stakerTx.Unsigned)
	}

	nodeID := staker.NodeID()
	primaryNetworkValidator, err := o.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		nodeID,
	)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errFailedFetchingPrimaryStaker, err)
	}

	expectedUptimePercentage := o.primaryUptimePercentage
	if subnetID := staker.SubnetID(); subnetID != constants.PrimaryNetworkID {
		transformSubnet, err := executor.GetTransformSubnetTx(o.state, subnetID)
		if err != nil {
			return false, fmt.Errorf("%w: %w", errFailedFetchingSubnetTransformation, err)
		}

		expectedUptimePercentage = float64(transformSubnet.UptimeRequirement) / reward.PercentDenominator
	} else if o.upgradeConfig.IsHeliconActivated(primaryNetworkValidator.StartTime) {
		// ACP-267 requires 90% uptime for Primary Network validators that start at
		// or after Helicon activation.
		expectedUptimePercentage = genesis.ACP267UptimeRequirement
	}

	uptime, err := o.uptimes.CalculateUptimePercentFrom(
		nodeID,
		primaryNetworkValidator.StartTime,
	)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errFailedCalculatingUptime, err)
	}

	return uptime >= expectedUptimePercentage, nil
}
