// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// TestStandardExecutorWrongTxType verifies that the standard executor rejects
// every tx type that must be executed by the proposal executor.
func TestStandardExecutorWrongTxType(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	tests := []platform.UnsignedTx{
		&platform.AdvanceTimeTx{},
		&platform.RewardValidatorTx{},
		&platform.RewardAutoRenewedValidatorTx{},
	}

	for _, unsignedTx := range tests {
		diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(t, err)

		feeCalculator := state.PickFeeCalculator(env.config, diff)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			&platform.Tx{Unsigned: unsignedTx},
			diff,
		)
		require.ErrorIs(t, err, errWrongTxType)
	}
}

// TestStandardExecutorBaseTxErrors verifies the failure cases of [platform.BaseTx]
// execution.
func TestStandardExecutorBaseTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "invalid_prior_to_durango",
			updateState: func(diff *state.Diff) {
				diff.SetTimestamp(env.config.UpgradeConfig.DurangoTime.Add(-1 * time.Second))
			},
			want: durango.errNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.BaseTx).BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.BaseTx).Memo = []byte("memo!")
			},
			want: avax.ErrMemoTooLarge,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.BaseTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: utxo.ErrInsufficientUnlockedFunds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The tx spends AVAX, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueBaseTx(
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          units.Avax,
						OutputOwners: *newOwner(),
					},
				}},
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

func TestStandardExecutorAddPermissionlessValidatorTxFlowCheckFails(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	wallet := newWallet(t, env, walletConfig{keys: genesistest.DefaultFundedKeys[:1]})
	feeCalculator := state.PickFeeCalculator(env.config, env.state)

	_, pop := newSignerWithPoP(t)

	endTime := env.state.GetTimestamp().Add(defaultMinStakingDuration)
	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(endTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		env.ctx.AVAXAssetID,
		newOwner(),
		newOwner(),
		reward.PercentDenominator,
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	// Remove the UTXOs the tx consumes so the flow check fails.
	for utxoID := range tx.InputIDs() {
		diff.DeleteUTXO(utxoID)
	}

	_, _, _, gotErr := StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		diff,
	)

	require.ErrorIs(gotErr, errFlowCheckFailed)
}

// TestStandardExecutorBaseTx verifies the successful execution of a
// [platform.BaseTx].
func TestStandardExecutorBaseTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	wallet := newWallet(t, env, walletConfig{})

	stx, err := wallet.IssueBaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          units.Avax,
				OutputOwners: *newOwner(),
			},
		}},
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)
}
