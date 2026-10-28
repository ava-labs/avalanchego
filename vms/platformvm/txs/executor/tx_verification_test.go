// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// TestExecutorsRunVerifyTx ensures that every execution method of the standard
// and proposal executors runs verifyTx: a tx that was never initialized must
// be rejected with [platform.ErrSignedTxNotInitialized] by the executor it
// belongs to.
//
// Each tx is executed at a fork where its type is allowed on that path, so
// that the upgrade gate of verifyTx does not fire first.
func TestExecutorsRunVerifyTx(t *testing.T) {
	executeStandardTx := func(env *environment, tx *platform.Tx) error {
		t.Helper()

		onAcceptState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(t, err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		return err
	}

	executeProposalTx := func(env *environment, tx *platform.Tx) error {
		t.Helper()

		onCommitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(t, err)
		onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(t, err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		return ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
	}

	tests := []struct {
		unsigned platform.UnsignedTx
		fork     upgradetest.Fork
		execute  func(env *environment, tx *platform.Tx) error
	}{
		// Proposal txs.
		{
			unsigned: &platform.AddValidatorTx{},
			fork:     upgradetest.ApricotPhase5,
			execute:  executeProposalTx,
		},
		{
			unsigned: &platform.AddSubnetValidatorTx{},
			fork:     upgradetest.ApricotPhase5,
			execute:  executeProposalTx,
		},
		{
			unsigned: &platform.AddDelegatorTx{},
			fork:     upgradetest.ApricotPhase5,
			execute:  executeProposalTx,
		},
		{
			unsigned: &platform.AdvanceTimeTx{},
			fork:     upgradetest.Latest,
			execute:  executeProposalTx,
		},
		{
			unsigned: &platform.RewardValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeProposalTx,
		},
		{
			unsigned: &platform.RewardAutoRenewedValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeProposalTx,
		},
		// Standard txs.
		{
			unsigned: &platform.AddValidatorTx{},
			fork:     upgradetest.Cortina,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.AddSubnetValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.AddDelegatorTx{},
			fork:     upgradetest.Cortina,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.CreateChainTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.CreateSubnetTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.ImportTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.ExportTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.RemoveSubnetValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.TransformSubnetTx{},
			fork:     upgradetest.Durango,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.AddPermissionlessValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.AddPermissionlessDelegatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.TransferSubnetOwnershipTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.BaseTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.ConvertSubnetToL1Tx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.RegisterL1ValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.SetL1ValidatorWeightTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.IncreaseL1ValidatorBalanceTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.DisableL1ValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.AddAutoRenewedValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
		{
			unsigned: &platform.SetAutoRenewedValidatorConfigTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardTx,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T_%s", tt.unsigned, tt.fork.String()), func(t *testing.T) {
			env := newEnvironment(t, tt.fork)

			// The tx is never initialized, so its ID is empty.
			tx := &platform.Tx{Unsigned: tt.unsigned}
			err := tt.execute(env, tx)
			require.ErrorIs(t, err, platform.ErrSignedTxNotInitialized)
		})
	}
}
