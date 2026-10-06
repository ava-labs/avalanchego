// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// TestExecutorsRunVerifyTx ensures that every execution method of the standard
// and proposal executors runs verifyTx: a tx that was never initialized must
// be rejected with [platform.ErrSignedTxNotInitialized] by the executor it
// belongs to.
//
// Each tx is executed at a fork where its type is allowed on that path, so
// that the upgrade gate of verifyTx does not fire first.
func TestExecutorsRunVerifyTx(t *testing.T) {
	tests := []struct {
		txType   string // platform.TxVisitor method name
		unsigned platform.UnsignedTx
		fork     upgradetest.Fork
		execute  func(t *testing.T, env *environment, tx *platform.Tx) error
	}{
		// Proposal txs.
		{
			txType:   "AddValidatorTx",
			unsigned: &platform.AddValidatorTx{},
			fork:     upgradetest.ApricotPhase5,
			execute:  executeProposalCharTx,
		},
		{
			txType:   "AddSubnetValidatorTx",
			unsigned: &platform.AddSubnetValidatorTx{},
			fork:     upgradetest.ApricotPhase5,
			execute:  executeProposalCharTx,
		},
		{
			txType:   "AddDelegatorTx",
			unsigned: &platform.AddDelegatorTx{},
			fork:     upgradetest.ApricotPhase5,
			execute:  executeProposalCharTx,
		},
		{
			txType:   "AdvanceTimeTx",
			unsigned: &platform.AdvanceTimeTx{},
			fork:     upgradetest.Latest,
			execute:  executeProposalCharTx,
		},
		{
			txType:   "RewardValidatorTx",
			unsigned: &platform.RewardValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeProposalCharTx,
		},
		{
			txType:   "RewardAutoRenewedValidatorTx",
			unsigned: &platform.RewardAutoRenewedValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeProposalCharTx,
		},
		// Standard txs.
		{
			txType:   "AddValidatorTx",
			unsigned: &platform.AddValidatorTx{},
			fork:     upgradetest.Cortina,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "AddSubnetValidatorTx",
			unsigned: &platform.AddSubnetValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "AddDelegatorTx",
			unsigned: &platform.AddDelegatorTx{},
			fork:     upgradetest.Cortina,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "CreateChainTx",
			unsigned: &platform.CreateChainTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "CreateSubnetTx",
			unsigned: &platform.CreateSubnetTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "ImportTx",
			unsigned: &platform.ImportTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "ExportTx",
			unsigned: &platform.ExportTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "RemoveSubnetValidatorTx",
			unsigned: &platform.RemoveSubnetValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "TransformSubnetTx",
			unsigned: &platform.TransformSubnetTx{},
			fork:     upgradetest.Durango,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "AddPermissionlessValidatorTx",
			unsigned: &platform.AddPermissionlessValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "AddPermissionlessDelegatorTx",
			unsigned: &platform.AddPermissionlessDelegatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "TransferSubnetOwnershipTx",
			unsigned: &platform.TransferSubnetOwnershipTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "BaseTx",
			unsigned: &platform.BaseTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "ConvertSubnetToL1Tx",
			unsigned: &platform.ConvertSubnetToL1Tx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "RegisterL1ValidatorTx",
			unsigned: &platform.RegisterL1ValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "SetL1ValidatorWeightTx",
			unsigned: &platform.SetL1ValidatorWeightTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "IncreaseL1ValidatorBalanceTx",
			unsigned: &platform.IncreaseL1ValidatorBalanceTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "DisableL1ValidatorTx",
			unsigned: &platform.DisableL1ValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "AddAutoRenewedValidatorTx",
			unsigned: &platform.AddAutoRenewedValidatorTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
		{
			txType:   "SetAutoRenewedValidatorConfigTx",
			unsigned: &platform.SetAutoRenewedValidatorConfigTx{},
			fork:     upgradetest.Latest,
			execute:  executeStandardCharTx,
		},
	}

	covered := set.NewSet[string](len(tests))
	for _, tt := range tests {
		covered.Add(tt.txType)

		t.Run(tt.txType+"_"+tt.fork.String(), func(t *testing.T) {
			env := newEnvironment(t, tt.fork)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			// The tx is never initialized, so its ID is empty.
			tx := &platform.Tx{Unsigned: tt.unsigned}
			err := tt.execute(t, env, tx)
			require.ErrorIs(t, err, platform.ErrSignedTxNotInitialized)
		})
	}

	// Adding a tx type to platform.TxVisitor must add a row above.
	visitorType := reflect.TypeOf((*platform.TxVisitor)(nil)).Elem()
	for i := 0; i < visitorType.NumMethod(); i++ {
		require.Contains(t, covered, visitorType.Method(i).Name)
	}
}
