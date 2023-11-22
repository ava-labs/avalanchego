// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = (*ProposalTxPreference)(nil)

type ProposalTxPreference struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	State state.Chain
	Tx    *txs.Tx

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

func (e *ProposalTxPreference) AddValidatorTx(tx *txs.AddValidatorTx) error {
	e.PrefersCommit = true
	return nil
}

func (e *ProposalTxPreference) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	e.PrefersCommit = true
	return nil
}

func (e *ProposalTxPreference) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	e.PrefersCommit = true
	return nil
}

func (e *ProposalTxPreference) AdvanceTimeTx(tx *txs.AdvanceTimeTx) error {
	e.PrefersCommit = true
	return nil
}

func (e *ProposalTxPreference) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	stakerTx, _, err := e.State.GetTx(tx.TxID)
	if err == database.ErrNotFound {
		e.PrefersCommit = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get staker transaction to remove: %w", err)
	}

	staker, ok := stakerTx.Unsigned.(txs.Staker)
	if !ok {
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

	// handle option preference
	e.PrefersCommit, err = e.shouldBeRewarded(subnetID, primaryNetworkValidator)
	return err
}

// TODO: calculate subnet uptimes
func (e *ProposalTxPreference) shouldBeRewarded(subnetID ids.ID, primaryNetworkValidator *state.Staker) (bool, error) {
	expectedUptimePercentage := e.Config.UptimePercentage
	if subnetID != constants.PrimaryNetworkID {
		transformSubnet, err := GetTransformSubnetTx(e.State, subnetID)
		if err == nil {
			expectedUptimePercentage = float64(transformSubnet.UptimeRequirement) / reward.PercentDenominator
		} else if err != database.ErrNotFound {
			return false, fmt.Errorf("failed to calculate uptime: %w", err)
		}
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
