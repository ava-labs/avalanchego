// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = (*ProposalTxPreference)(nil)

type ProposalTxPreference struct {
	// inputs, to be filled before visitor methods are called
	PrimaryUptimePercentage float64
	Uptimes                 uptime.Manager
	State                   state.Chain
	Tx                      *txs.Tx

	// outputs populated by this struct's methods:
	//
	// [PrefersCommit] is true iff this node initially prefers to
	// commit this block transaction.
	PrefersCommit bool
}

func (*ProposalTxPreference) CreateChainTx(*txs.CreateChainTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) ImportTx(*txs.ImportTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) ExportTx(*txs.ExportTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) BaseTx(*txs.BaseTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) AddValidatorTx(*txs.AddValidatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return ErrWrongTxType
}

func (*ProposalTxPreference) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (e *ProposalTxPreference) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	stakerTx, _, err := e.State.GetTx(tx.TxID)
	if err == database.ErrNotFound {
		// This can happen if this transaction is attempting to reward a
		// validator that hasn't been confirmed.
		e.PrefersCommit = true
		return nil
	}
	if err != nil {
		// GetTx can only return [ErrNotFound], or an unexpected error like a
		// parsing error or internal DB error. For anything other than
		// [ErrNotFound] the block can just be dropped.
		return fmt.Errorf("failed to get staker transaction to remove: %w", err)
	}

	staker, ok := stakerTx.Unsigned.(txs.Staker)
	if !ok {
		// Because this transaction isn't guaranteed to have been verified yet,
		// it's possible that a malicious node issued this transaction into a
		// block that will fail verification in the future.
		return fmt.Errorf("staker transaction has unexpected type: %T", stakerTx.Unsigned)
	}

	subnetID := staker.SubnetID()
	nodeID := staker.NodeID()

	// retrieve primaryNetworkValidator before possibly removing it.
	primaryNetworkValidator, err := e.State.GetCurrentValidator(
		constants.PrimaryNetworkID,
		nodeID,
	)
	if err == database.ErrNotFound {
		// TODO: I don't think this should ever happen. This would imply that
		// the Commit or Abort blocks have already been accepted.
		e.PrefersCommit = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get primary network validator for validator to remove: %w", err)
	}

	expectedUptimePercentage := e.PrimaryUptimePercentage
	if subnetID != constants.PrimaryNetworkID {
		transformSubnet, err := GetTransformSubnetTx(e.State, subnetID)
		if err == nil {
			expectedUptimePercentage = float64(transformSubnet.UptimeRequirement) / reward.PercentDenominator
		} else if err != database.ErrNotFound {
			return fmt.Errorf("failed to calculate uptime: %w", err)
		}
		// If we couldn't find the transform subnet tx, so we default to the
		// primary network requirements. This is unlikely to happen during
		// normal execution because this would mean our node is trying to reward
		// a validator that hasn't been confirmed yet.
	}

	// handle option preference
	e.PrefersCommit, err = e.shouldBeRewarded(subnetID, primaryNetworkValidator)
	return err
}

// TODO: calculate subnet uptimes
func (e *ProposalTxPreference) shouldBeRewarded(subnetID ids.ID, primaryNetworkValidator *state.Staker) (bool, error) {
	expectedUptimePercentage := e.PrimaryUptimePercentage
	if subnetID != constants.PrimaryNetworkID {
		transformSubnet, err := GetTransformSubnetTx(e.State, subnetID)
		if err == nil {
			expectedUptimePercentage = float64(transformSubnet.UptimeRequirement) / reward.PercentDenominator
		} else if err != database.ErrNotFound {
			return false, fmt.Errorf("failed to calculate uptime: %w", err)
		}
		// If we couldn't find the transform subnet tx, so we default to the
		// primary network requirements. This is unlikely to happen during
		// normal execution because this would mean our node is trying to reward
		// a validator that hasn't been confirmed yet.
	}

	uptime, err := e.Uptimes.CalculateUptimePercentFrom(
		primaryNetworkValidator.NodeID,
		constants.PrimaryNetworkID,
		primaryNetworkValidator.StartTime,
	)
	if err == database.ErrNotFound {
		// TODO: I don't think this should ever happen. This would imply that
		// the Commit or Abort blocks have already been accepted.
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to calculate uptime: %w", err)
	}
	return uptime >= expectedUptimePercentage, nil
}
