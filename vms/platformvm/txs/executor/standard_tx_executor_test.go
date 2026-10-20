// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
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
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// This tests that the math performed during TransformSubnetTx execution can
// never overflow
const _ time.Duration = math.MaxUint32 * time.Second

var errTest = errors.New("non-nil error")

// Verifies that [AddValidatorTx] and [AddDelegatorTx] are disabled post-Durango
func TestDurangoDisabledTransactions(t *testing.T) {
	type test struct {
		name        string
		buildTx     func(t *testing.T, env *environment) *platform.Tx
		expectedErr error
	}

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tests := []test{
		{
			name: "AddValidatorTx",
			buildTx: func(t *testing.T, env *environment) *platform.Tx {
				var (
					nodeID    = ids.GenerateTestNodeID()
					chainTime = env.state.GetTimestamp()
					endTime   = chainTime.Add(defaultMaxStakingDuration)
				)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddValidatorTx(
					&platform.Validator{
						NodeID: nodeID,
						Start:  0,
						End:    uint64(endTime.Unix()),
						Wght:   defaultMinValidatorStake,
					},
					rewardsOwner,
					reward.PercentDenominator,
				)
				require.NoError(t, err)

				return tx
			},
			expectedErr: errAddValidatorTxPostDurango,
		},
		{
			name: "AddDelegatorTx",
			buildTx: func(t *testing.T, env *environment) *platform.Tx {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != platform.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddDelegatorTx(
					&platform.Validator{
						NodeID: primaryValidator.NodeID,
						Start:  0,
						End:    uint64(primaryValidator.EndTime.Unix()),
						Wght:   defaultMinValidatorStake,
					},
					rewardsOwner,
				)
				require.NoError(err)

				return tx
			},
			expectedErr: errAddDelegatorTxPostDurango,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.Durango)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			tx := tt.buildTx(t, env)
			feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				onAcceptState,
			)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

// Verifies that the Memo field is required to be empty post-Durango
func TestDurangoMemoField(t *testing.T) {
	type test struct {
		name      string
		setupTest func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff)
	}

	owners := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tests := []test{
		{
			name: "AddSubnetValidatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != platform.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})
				tx, err := wallet.IssueAddSubnetValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(primaryValidator.EndTime.Unix()),
							Wght:   defaultMinValidatorStake,
						},
						Subnet: subnetID,
					},
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "CreateChainTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueCreateChainTx(
					subnetID,
					[]byte{},
					ids.GenerateTestID(),
					[]ids.ID{},
					"aaa",
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "CreateSubnetTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueCreateSubnetTx(
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "ImportTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				var (
					sourceChain  = env.ctx.XChainID
					sourceKey    = genesistest.DefaultFundedKeys[1]
					sourceAmount = 10 * units.Avax
				)

				sharedMemory := fundedSharedMemory(
					t,
					env,
					sourceKey,
					sourceChain,
					map[ids.ID]uint64{
						env.ctx.AVAXAssetID: sourceAmount,
					},
					rand.NewSource(0),
				)
				env.msm.SharedMemory = sharedMemory

				wallet := newWallet(t, env, walletConfig{
					chainIDs: []ids.ID{sourceChain},
				})

				tx, err := wallet.IssueImportTx(
					sourceChain,
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "ExportTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueExportTx(
					env.ctx.XChainID,
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          units.Avax,
							OutputOwners: *owners,
						},
					}},
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "RemoveSubnetValidatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != platform.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				endTime := primaryValidator.EndTime

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})
				subnetValTx, err := wallet.IssueAddSubnetValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(endTime.Unix()),
							Wght:   genesistest.DefaultValidatorWeight,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)

				feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
				_, _, _, err = StandardTx(
					&env.backend,
					feeCalculator,
					subnetValTx,
					onAcceptState,
				)
				require.NoError(err)

				tx, err := wallet.IssueRemoveSubnetValidatorTx(
					primaryValidator.NodeID,
					subnetID,
					common.WithMemo(memoField),
				)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "TransformSubnetTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueTransformSubnetTx(
					subnetID,                  // subnetID
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
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "AddPermissionlessValidatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)
				var (
					nodeID    = ids.GenerateTestNodeID()
					chainTime = env.state.GetTimestamp()
					endTime   = chainTime.Add(defaultMaxStakingDuration)
				)
				sk, err := localsigner.New()
				require.NoError(err)
				pop, err := signer.NewProofOfPossession(sk)
				require.NoError(err)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddPermissionlessValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: nodeID,
							Start:  0,
							End:    uint64(endTime.Unix()),
							Wght:   env.config.MinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					pop,
					env.ctx.AVAXAssetID,
					owners,
					owners,
					reward.PercentDenominator,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "AddPermissionlessDelegatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != platform.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddPermissionlessDelegatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(primaryValidator.EndTime.Unix()),
							Wght:   defaultMinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					env.ctx.AVAXAssetID,
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "TransferSubnetOwnershipTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueTransferSubnetOwnershipTx(
					subnetID,
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "BaseTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*platform.Tx, *state.Diff) {
				require := require.New(t)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueBaseTx(
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{ids.ShortEmpty},
								},
							},
						},
					},
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.Durango)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			feeCalculator := state.PickFeeCalculator(env.config, env.state)

			// Populated memo field should error
			tx, onAcceptState := tt.setupTest(t, env, []byte{'m', 'e', 'm', 'o'})
			_, _, _, err := StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				onAcceptState,
			)
			require.ErrorIs(err, avax.ErrMemoTooLarge)

			// Empty memo field should not error
			tx, onAcceptState = tt.setupTest(t, env, []byte{})
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				onAcceptState,
			)
			require.NoError(err)
		})
	}
}

// Verifies that [TransformSubnetTx] is disabled post-Etna
func TestEtnaDisabledTransactions(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Etna)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	tx := &platform.Tx{
		Unsigned: &platform.TransformSubnetTx{},
	}
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		onAcceptState,
	)
	require.ErrorIs(err, errTransformSubnetTxPostEtna)
}

func TestStandardExecutorAddPermissionlessValidatorTxFlowCheckFails(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	wallet := newWallet(t, env, walletConfig{keys: genesistest.DefaultFundedKeys[:1]})
	feeCalculator := state.PickFeeCalculator(env.config, env.state)

	sk, err := localsigner.New()
	require.NoError(err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
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
		rewardsOwner,
		rewardsOwner,
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
