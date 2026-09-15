// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// Verifies that [platform.AddValidatorTx] and [platform.AddDelegatorTx] are disabled post-Durango
func TestDurangoDisabledTransactions(t *testing.T) {
	env := newEnvironment(t, upgradetest.Durango)
	wallet := newWallet(t, env, walletConfig{})

	validator := &platform.Validator{
		NodeID: ids.GenerateTestNodeID(),
		Start:  genesistest.DefaultValidatorStartTimeUnix,
		End:    genesistest.DefaultValidatorEndTimeUnix,
		Wght:   env.config.MinValidatorStake,
	}
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tests := []struct {
		name string
		tx   func() (*platform.Tx, error)
		want error
	}{
		{
			name: "AddValidatorTx",
			tx: func() (*platform.Tx, error) {
				return wallet.IssueAddValidatorTx(validator, rewardsOwner, reward.PercentDenominator)
			},
			want: errAddValidatorTxPostDurango,
		},
		{
			name: "AddDelegatorTx",
			tx: func() (*platform.Tx, error) {
				return wallet.IssueAddDelegatorTx(validator, rewardsOwner)
			},
			want: errAddDelegatorTxPostDurango,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			tx, err := tt.tx()
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)
			require.ErrorIs(err, tt.want)
		})
	}
}

// TestDurangoMemoField verifies that post-Durango, txs with a non-empty memo
// field are rejected.
func TestDurangoMemoField(t *testing.T) {
	env := newEnvironment(t, upgradetest.Durango)
	wallet := newWallet(t, env, walletConfig{})

	owners := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	memoOpt := common.WithMemo([]byte{'m'})

	tests := []struct {
		tx func() (*platform.Tx, error)
	}{
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueAddSubnetValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: ids.GenerateTestNodeID(),
							Start:  uint64(env.state.GetTimestamp().Unix()),
							End:    uint64(env.state.GetTimestamp().Add(env.config.MinStakeDuration).Unix()),
							Wght:   env.config.MinValidatorStake,
						},
						Subnet: testSubnet1.ID(),
					},
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueCreateChainTx(
					testSubnet1.ID(),
					nil,
					ids.GenerateTestID(),
					nil,
					"aaa",
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueCreateSubnetTx(owners, memoOpt)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				var (
					sourceChain  = env.ctx.XChainID
					sourceKey    = genesistest.DefaultFundedKeys[1]
					sourceAmount = 10 * units.Avax
				)

				env.msm.SharedMemory = fundedSharedMemory(
					t,
					env,
					sourceKey,
					sourceChain,
					map[ids.ID]uint64{
						env.ctx.AVAXAssetID: sourceAmount,
					},
					rand.NewSource(0),
				)

				wallet := newWallet(t, env, walletConfig{
					chainIDs: []ids.ID{sourceChain},
				})
				return wallet.IssueImportTx(
					sourceChain,
					owners,
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueExportTx(
					env.ctx.XChainID,
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          units.Avax,
							OutputOwners: *owners,
						},
					}},
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueRemoveSubnetValidatorTx(
					genesistest.DefaultNodeIDs[0],
					testSubnet1.ID(),
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueTransformSubnetTx(
					testSubnet1.ID(),          // subnetID
					ids.GenerateTestID(),      // assetID
					10,                        // initial supply
					10,                        // max supply
					0,                         // min consumption rate
					reward.PercentDenominator, // max consumption rate
					2,                         // min validator stake
					10,                        // max validator stake
					time.Minute,               // min stake duration
					time.Hour,                 // max stake duration
					1,                         // min delegation fees
					10,                        // min delegator stake
					1,                         // max validator weight factor
					80,                        // uptime requirement
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				sk, err := localsigner.New()
				if err != nil {
					return nil, err
				}
				pop, err := signer.NewProofOfPossession(sk)
				if err != nil {
					return nil, err
				}

				return wallet.IssueAddPermissionlessValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: ids.GenerateTestNodeID(),
							End:    uint64(env.state.GetTimestamp().Add(env.config.MaxStakeDuration).Unix()),
							Wght:   env.config.MinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					pop,
					env.ctx.AVAXAssetID,
					owners,
					owners,
					reward.PercentDenominator,
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueAddPermissionlessDelegatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: genesistest.DefaultNodeIDs[0],
							Start:  0,
							End:    genesistest.DefaultValidatorEndTimeUnix,
							Wght:   env.config.MinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					env.ctx.AVAXAssetID,
					owners,
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueTransferSubnetOwnershipTx(
					testSubnet1.ID(),
					owners,
					memoOpt,
				)
			},
		},
		{
			tx: func() (*platform.Tx, error) {
				return wallet.IssueBaseTx(
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          1,
							OutputOwners: *owners,
						},
					}},
					memoOpt,
				)
			},
		},
	}

	for _, tt := range tests {
		tx, err := tt.tx()
		require.NoError(t, err)

		name := fmt.Sprintf("%T", tx.Unsigned)
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)
			require.ErrorIs(err, avax.ErrMemoTooLarge)
		})
	}
}

// Verifies that [platform.TransformSubnetTx] is disabled post-Etna
func TestEtnaDisabledTransactions(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Etna)
	wallet := newWallet(t, env, walletConfig{})

	tx, err := wallet.IssueTransformSubnetTx(
		testSubnet1.ID(),
		ids.GenerateTestID(),
		10,
		10,
		0,
		reward.PercentDenominator,
		2,
		10,
		time.Minute,
		time.Hour,
		1,
		10,
		1,
		80,
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		diff,
	)
	require.ErrorIs(err, errTransformSubnetTxPostEtna)
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
			want: errDurangoUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.BaseTx).BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
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
						Amt: units.Avax,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						},
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
				Amt: units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
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
