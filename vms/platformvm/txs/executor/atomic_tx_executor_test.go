// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestAtomicExecutorWrongTxTypes(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)

	utxs := []txs.UnsignedTx{
		&txs.AddValidatorTx{},
		&txs.AddSubnetValidatorTx{},
		&txs.AddDelegatorTx{},
		&txs.CreateChainTx{},
		&txs.CreateSubnetTx{},
		&txs.AdvanceTimeTx{},
		&txs.RewardValidatorTx{},
		&txs.RemoveSubnetValidatorTx{},
		&txs.TransformSubnetTx{},
		&txs.AddPermissionlessValidatorTx{},
		&txs.AddPermissionlessDelegatorTx{},
		&txs.TransferSubnetOwnershipTx{},
		&txs.BaseTx{},
		&txs.ConvertSubnetToL1Tx{},
		&txs.RegisterL1ValidatorTx{},
		&txs.SetL1ValidatorWeightTx{},
		&txs.IncreaseL1ValidatorBalanceTx{},
		&txs.DisableL1ValidatorTx{},
		&txs.StopContinuousValidatorTx{},
		&txs.AddContinuousValidatorTx{},
		&txs.RewardContinuousValidatorTx{},
	}

	for _, utx := range utxs {
		name := fmt.Sprintf("wrong tx type %T", utx)
		t.Run(name, func(t *testing.T) {
			_, _, _, err := AtomicTx(
				&env.backend,
				state.PickFeeCalculator(env.config, env.state),
				ids.GenerateTestID(),
				env,
				&txs.Tx{Unsigned: utx},
			)
			require.ErrorIs(err, ErrWrongTxType)
		})
	}
}
