// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// TestStandardExecutorAddAutoRenewedValidatorTx verifies the successful
// execution of an [platform.AddAutoRenewedValidatorTx].
func TestStandardExecutorAddAutoRenewedValidatorTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	wallet := newWallet(t, env, walletConfig{})
	feeCalculator := state.PickFeeCalculator(env.config, env.state)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()
	period := 2 * env.config.MinStakeDuration
	weight := 2 * env.config.MinValidatorStake
	configOwner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}

	addAutoRenewedTx, err := wallet.IssueAddAutoRenewedValidatorTx(
		nodeID,
		weight,
		pop,
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		configOwner,
		100_000,
		200_000,
		period,
	)
	require.NoError(t, err)

	currentSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	rewards, err := GetRewardsCalculator(
		env.config.RewardConfig,
		env.config.UpgradeConfig,
		env.state,
		constants.PrimaryNetworkID,
	)
	require.NoError(t, err)
	wantPotentialReward := rewards.Calculate(
		env.state.GetTimestamp(),
		period,
		weight,
		currentSupply,
	)

	// Input UTXOs are present before execution
	inputIDs := addAutoRenewedTx.InputIDs()
	require.NotEmpty(t, inputIDs)
	for utxoID := range inputIDs {
		_, err := env.state.GetUTXO(utxoID)
		require.NoError(t, err)
	}

	// Output UTXOs are not present before execution
	baseTxOutputUTXOs := addAutoRenewedTx.UTXOs()
	require.NotEmpty(t, baseTxOutputUTXOs)
	for _, utxo := range baseTxOutputUTXOs {
		_, err := env.state.GetUTXO(utxo.InputID())
		require.ErrorIs(t, err, database.ErrNotFound)
	}

	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		addAutoRenewedTx,
		diff,
	)
	require.NoError(t, err)
	require.True(t, addAutoRenewedTx.Unsigned.(*platform.AddAutoRenewedValidatorTx).BaseTx.SyntacticallyVerified)
	requireBaseTxApplied(t, env, diff, feeCalculator, addAutoRenewedTx)
	require.NoError(t, diff.Apply(env.state))

	validator, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)

	wantValidator := &state.Staker{
		TxID:            addAutoRenewedTx.TxID,
		NodeID:          nodeID,
		PublicKey:       sk.PublicKey(),
		SubnetID:        constants.PrimaryNetworkID,
		Weight:          weight,
		StartTime:       env.state.GetTimestamp(),
		EndTime:         env.state.GetTimestamp().Add(period),
		PotentialReward: wantPotentialReward,
		NextTime:        env.state.GetTimestamp().Add(period),
		Priority:        platform.PrimaryNetworkValidatorCurrentPriority,
	}
	require.Equal(t, wantValidator, validator)

	delegatorIt, err := env.state.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.Empty(t, iterator.ToSlice(delegatorIt))

	stakerIt, err := env.state.GetCurrentStakerIterator()
	require.NoError(t, err)
	require.Contains(t, iterator.ToSlice(stakerIt), wantValidator)

	stakingInfo, err := env.state.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)

	wantStakingInfo := state.StakingInfo{
		DelegateeReward:          0,
		AccruedValidationRewards: 0,
		AccruedDelegateeRewards:  0,
		AutoCompoundRewardShares: 200_000,
		NextPeriod:               uint64(period / time.Second),
	}
	require.Equal(t, wantStakingInfo, stakingInfo)
}

// TestStandardExecutorAddAutoRenewedValidatorTxErrors verifies the failure
// cases of [platform.AddAutoRenewedValidatorTx] execution.
func TestStandardExecutorAddAutoRenewedValidatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	wallet := newWallet(t, env, walletConfig{})
	feeCalculator := state.PickFeeCalculator(env.config, env.state)

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "invalid_upgrade",
			updateState: func(diff *state.Diff) {
				diff.SetTimestamp(env.backend.Config.UpgradeConfig.HeliconTime.Add(-1 * time.Second))
			},
			want: errHeliconUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).Memo = []byte("memo!")
			},
			want: avax.ErrMemoTooLarge,
		},
		{
			name: "weight_too_small",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).StakeOuts[0].Out.(*secp256k1fx.TransferOutput).Amt = env.config.MinValidatorStake - 1
			},
			want: errWeightTooSmall,
		},
		{
			name: "weight_too_large",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).StakeOuts[0].Out.(*secp256k1fx.TransferOutput).Amt = env.config.MaxValidatorStake + 1
			},
			want: errWeightTooLarge,
		},
		{
			name: "insufficient_delegation_fee",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).DelegationShares = env.config.MinDelegationFee - 1
			},
			want: errInsufficientDelegationFee,
		},
		{
			name: "stake_too_short",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).Period = uint64(env.config.HeliconMinStakeDuration.Seconds()) - 1
			},
			want: errStakeTooShort,
		},
		{
			name: "stake_too_long",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).Period = uint64(env.config.MaxStakeDuration.Seconds()) + 1
			},
			want: ErrStakeTooLong,
		},
		{
			name: "duplicate_validator",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddAutoRenewedValidatorTx).ValidatorNodeID = genesistest.DefaultNodeIDs[0].Bytes()
			},
			want: ErrDuplicateValidator,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sk, got := localsigner.New()
			require.NoError(t, got)

			pop, got := signer.NewProofOfPossession(sk)
			require.NoError(t, got)

			tx, got := wallet.IssueAddAutoRenewedValidatorTx(
				ids.GenerateTestNodeID(),
				env.config.MinValidatorStake,
				pop,
				&secp256k1fx.OutputOwners{},
				&secp256k1fx.OutputOwners{},
				&secp256k1fx.OutputOwners{},
				500_000,
				300_000,
				env.config.MinStakeDuration,
			)
			require.NoError(t, got)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

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

// TestStandardExecutorSetAutoRenewedValidatorConfigTx verifies the successful
// execution of a [platform.SetAutoRenewedValidatorConfigTx].
func TestStandardExecutorSetAutoRenewedValidatorConfigTx(t *testing.T) {
	const (
		delegationShares            = 0.5 * reward.PercentDenominator
		autoCompoundRewardShares    = 0.3 * reward.PercentDenominator
		newAutoCompoundRewardShares = 0.3 * reward.PercentDenominator
	)

	tests := []struct {
		name      string
		newPeriod time.Duration
	}{
		{
			name:      "updated_period_and_auto-compound_reward_shares",
			newPeriod: 30 * 24 * time.Hour,
		},
		{
			name:      "period_0_(exit_requested)",
			newPeriod: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)
			// Charge a non-zero fee so the config tx is funded with a real input
			// and change output, letting us assert UTXO consumption/production below.
			env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig

			var (
				wallet        = newWallet(t, env, walletConfig{})
				feeCalculator = state.PickFeeCalculator(env.config, env.state)
			)

			sk, err := localsigner.New()
			require.NoError(t, err)

			pop, err := signer.NewProofOfPossession(sk)
			require.NoError(t, err)

			nodeID := ids.GenerateTestNodeID()

			addAutoRenewedValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
				nodeID,
				env.config.MinValidatorStake,
				pop,
				&secp256k1fx.OutputOwners{},
				&secp256k1fx.OutputOwners{},
				&secp256k1fx.OutputOwners{},
				delegationShares,
				autoCompoundRewardShares,
				2*env.config.MinStakeDuration,
			)
			require.NoError(t, err)
			validatorTx := addAutoRenewedValidatorTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)

			// Execute the AddAutoRenewedValidatorTx so the validator and the UTXOs
			// it spends/creates are reflected in env.state. This keeps env.state in
			// sync with the wallet's UTXO set, so the config tx is funded with a UTXO
			// that actually exists during execution.
			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			_, _, _, err = StandardTx(&env.backend, feeCalculator, addAutoRenewedValidatorTx, diff)
			require.NoError(t, err)

			// Record the tx so the config tx can look it up by ID during verification.
			diff.AddTx(addAutoRenewedValidatorTx, status.Committed)
			require.NoError(t, diff.Apply(env.state))

			// Seed non-zero values to verify they're preserved by the config update.
			initialStakingInfo := state.StakingInfo{
				DelegateeReward:          1,
				AccruedValidationRewards: 2,
				AccruedDelegateeRewards:  3,
				AutoCompoundRewardShares: 100_000,
				NextPeriod:               validatorTx.Period,
			}

			setAutoRenewedValidatorConfigTx, err := wallet.IssueSetAutoRenewedValidatorConfigTx(
				addAutoRenewedValidatorTx.TxID,
				newAutoCompoundRewardShares,
				tt.newPeriod,
			)
			require.NoError(t, err)

			// Before execution: the input UTXOs exist and the output UTXOs don't.
			inputIDs := setAutoRenewedValidatorConfigTx.InputIDs()
			require.NotEmpty(t, inputIDs)
			for inputID := range inputIDs {
				_, err := env.state.GetUTXO(inputID)
				require.NoError(t, err)
			}

			outputUTXOs := setAutoRenewedValidatorConfigTx.UTXOs()
			require.NotEmpty(t, outputUTXOs)
			for _, outputUTXO := range outputUTXOs {
				_, err := env.state.GetUTXO(outputUTXO.InputID())
				require.ErrorIs(t, err, database.ErrNotFound)
			}

			diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)
			require.NoError(t, diff.SetStakingInfo(constants.PrimaryNetworkID, nodeID, initialStakingInfo))

			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				setAutoRenewedValidatorConfigTx,
				diff,
			)

			require.NoError(t, err)
			require.True(t, setAutoRenewedValidatorConfigTx.Unsigned.(*platform.SetAutoRenewedValidatorConfigTx).BaseTx.SyntacticallyVerified)
			requireBaseTxApplied(t, env, diff, feeCalculator, setAutoRenewedValidatorConfigTx)
			require.NoError(t, diff.Apply(env.state))

			stakingInfo, err := env.state.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
			require.NoError(t, err)

			wantStakingInfo := initialStakingInfo
			wantStakingInfo.AutoCompoundRewardShares = newAutoCompoundRewardShares
			wantStakingInfo.NextPeriod = uint64(tt.newPeriod.Seconds())
			require.Equal(t, wantStakingInfo, stakingInfo)
		})
	}
}

func TestStandardExecutorSetAutoRenewedValidatorConfigTxErrors(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Latest)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	it, err := env.state.GetCurrentStakerIterator()
	require.NoError(t, err)

	validators := iterator.ToSlice(it)
	require.NotEmpty(t, validators)
	fixedStakerTxID := validators[0].TxID

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()

	addPastContValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
		nodeID,
		env.config.MinValidatorStake,
		pop,
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		0,
		0,
		env.config.MinStakeDuration,
	)
	require.NoError(t, err)

	addAutoRenewedValidatorTx, err := wallet.IssueAddAutoRenewedValidatorTx(
		nodeID,
		env.config.MinValidatorStake,
		pop,
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()}},
		500_000,
		300_000,
		2*env.config.MinStakeDuration,
	)
	require.NoError(t, err)

	validatorTx := addAutoRenewedValidatorTx.Unsigned.(*platform.AddAutoRenewedValidatorTx)

	startTime := time.Unix(int64(genesistest.DefaultValidatorStartTimeUnix+1), 0)
	duration := time.Duration(validatorTx.Period) * time.Second
	staker, err := state.NewCurrentStaker(
		addAutoRenewedValidatorTx.ID(),
		validatorTx,
		startTime,
		startTime.Add(duration),
		validatorTx.Weight(),
		0,
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)
	diff.AddTx(addPastContValidatorTx, status.Committed)
	diff.AddTx(addAutoRenewedValidatorTx, status.Committed)
	require.NoError(t, diff.PutCurrentValidator(staker))
	require.NoError(t, diff.Apply(env.state))

	tests := []struct {
		name        string
		updateTx    func(testing.TB, *platform.SetAutoRenewedValidatorConfigTx, *platform.Tx)
		updateState func(testing.TB, *state.Diff)
		wantErr     error
	}{
		{
			name: "invalid_upgrade",
			updateState: func(_ testing.TB, diff *state.Diff) {
				diff.SetTimestamp(env.backend.Config.UpgradeConfig.HeliconTime.Add(-1 * time.Second))
			},
			wantErr: errHeliconUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(_ testing.TB, _ *platform.SetAutoRenewedValidatorConfigTx, stx *platform.Tx) {
				stx.Unsigned.(*platform.SetAutoRenewedValidatorConfigTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			wantErr: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(_ testing.TB, tx *platform.SetAutoRenewedValidatorConfigTx, _ *platform.Tx) {
				tx.Memo = []byte("memo!")
			},
			wantErr: avax.ErrMemoTooLarge,
		},
		{
			name: "stopped_validator",
			updateState: func(t testing.TB, diff *state.Diff) {
				require.NoError(t, diff.DeleteCurrentValidator(&state.Staker{
					TxID:     addAutoRenewedValidatorTx.ID(),
					NodeID:   nodeID,
					SubnetID: constants.PrimaryNetworkID,
				}))
			},
			wantErr: database.ErrNotFound,
		},
		{
			name: "missing_staker_tx",
			updateTx: func(_ testing.TB, tx *platform.SetAutoRenewedValidatorConfigTx, _ *platform.Tx) {
				tx.TxID = ids.GenerateTestID()
			},
			wantErr: database.ErrNotFound,
		},
		{
			name: "invalid_staker_tx",
			updateTx: func(_ testing.TB, tx *platform.SetAutoRenewedValidatorConfigTx, _ *platform.Tx) {
				tx.TxID = addPastContValidatorTx.ID()
			},
			wantErr: errInvalidStakerTx,
		},
		{
			name: "invalid_staker_tx_type",
			updateTx: func(_ testing.TB, tx *platform.SetAutoRenewedValidatorConfigTx, _ *platform.Tx) {
				tx.TxID = fixedStakerTxID
			},
			wantErr: errInvalidStakerTxType,
		},
		{
			name: "stake_too_short",
			updateTx: func(_ testing.TB, tx *platform.SetAutoRenewedValidatorConfigTx, _ *platform.Tx) {
				tx.Period = uint64(env.config.HeliconMinStakeDuration.Seconds()) - 1
			},
			wantErr: errStakeTooShort,
		},
		{
			name: "stake_too_long",
			updateTx: func(_ testing.TB, tx *platform.SetAutoRenewedValidatorConfigTx, _ *platform.Tx) {
				tx.Period = uint64(env.config.MaxStakeDuration.Seconds()) + 1
			},
			wantErr: ErrStakeTooLong,
		},
		{
			name: "invalid_auth",
			updateTx: func(t testing.TB, tx *platform.SetAutoRenewedValidatorConfigTx, sTx *platform.Tx) {
				dummySig, err := genesistest.DefaultFundedKeys[1].SignHash([]byte{})
				require.NoError(t, err)

				tx.Auth = &secp256k1fx.Input{SigIndices: []uint32{0}}
				sTx.Creds = []verify.Verifiable{&secp256k1fx.Credential{}, &secp256k1fx.Credential{
					Sigs: [][secp256k1.SignatureLen]byte{[secp256k1.SignatureLen]byte(dummySig)},
				}}
			},
			wantErr: secp256k1fx.ErrWrongSig,
		},
		{
			name: "wrong_number_of_credentials",
			updateTx: func(_ testing.TB, _ *platform.SetAutoRenewedValidatorConfigTx, sTx *platform.Tx) {
				sTx.Creds = nil
			},
			wantErr: errWrongNumberOfCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			tx, err := wallet.IssueSetAutoRenewedValidatorConfigTx(addAutoRenewedValidatorTx.ID(), 0, 0)
			require.NoError(t, err)

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			if tt.updateTx != nil {
				tt.updateTx(t, tx.Unsigned.(*platform.SetAutoRenewedValidatorConfigTx), tx)
			}

			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestStandardExecutorRewardAutoRenewedValidatorTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		newRewardAutoRenewedValidatorTx(t, ids.GenerateTestID(), time.Unix(1, 0)),
		diff,
	)
	require.ErrorIs(t, err, errWrongTxType)
}
