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
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo/utxomock"
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

// Returns a TransformSubnetTx that passes syntactic verification.
// Memo field is empty as required post Durango activation
func newTransformSubnetTx(t *testing.T) (*platform.TransformSubnetTx, *platform.Tx) {
	t.Helper()

	creds := []verify.Verifiable{
		&secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
		&secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
	}
	unsignedTx := &platform.TransformSubnetTx{
		BaseTx: platform.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID: ids.GenerateTestID(),
					},
					Asset: avax.Asset{
						ID: ids.GenerateTestID(),
					},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{0, 1},
						},
					},
				}},
				Outs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: 1,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
							},
						},
					},
				},
			},
		},
		Subnet:                   ids.GenerateTestID(),
		AssetID:                  ids.GenerateTestID(),
		InitialSupply:            10,
		MaximumSupply:            10,
		MinConsumptionRate:       0,
		MaxConsumptionRate:       reward.PercentDenominator,
		MinValidatorStake:        2,
		MaxValidatorStake:        10,
		MinStakeDuration:         1,
		MaxStakeDuration:         2,
		MinDelegationFee:         reward.PercentDenominator,
		MinDelegatorStake:        1,
		MaxValidatorWeightFactor: 1,
		UptimeRequirement:        reward.PercentDenominator,
		SubnetAuth: &secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
	}
	tx := &platform.Tx{
		Unsigned: unsignedTx,
		Creds:    creds,
	}
	require.NoError(t, tx.Initialize(platform.Codec))
	return unsignedTx, tx
}

// mock implementations that can be used in tests
// for verifying TransformSubnetTx.
type transformSubnetTxVerifyEnv struct {
	latestForkTime time.Time
	fx             *fxmock.Fx
	flowChecker    *utxomock.Verifier
	unsignedTx     *platform.TransformSubnetTx
	tx             *platform.Tx
	state          *state.Diff
}

// Returns mock implementations that can be used in tests
// for verifying TransformSubnetTx.
func newValidTransformSubnetTxVerifyEnv(t *testing.T, ctrl *gomock.Controller) transformSubnetTxVerifyEnv {
	t.Helper()

	now := time.Now()
	mockFx := fxmock.NewFx(ctrl)
	mockFlowChecker := utxomock.NewVerifier(ctrl)
	unsignedTx, tx := newTransformSubnetTx(t)
	s := statetest.New(t, statetest.Config{})
	stateDiff, err := state.NewDiffOn(s, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	return transformSubnetTxVerifyEnv{
		latestForkTime: now,
		fx:             mockFx,
		flowChecker:    mockFlowChecker,
		unsignedTx:     unsignedTx,
		tx:             tx,
		state:          stateDiff,
	}
}

func TestStandardExecutorTransformSubnetTx(t *testing.T) {
	type test struct {
		name            string
		newExecutor     func(*gomock.Controller) (*platform.TransformSubnetTx, *standardTxExecutor)
		stateAssertions func(*testing.T, *state.Diff, *platform.Tx)
		err             error
	}

	tests := []test{
		{
			name: "tx fails syntactic verification",
			newExecutor: func(ctrl *gomock.Controller) (*platform.TransformSubnetTx, *standardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				// Setting the tx to nil makes the tx fail syntactic verification
				env.tx.Unsigned = (*platform.TransformSubnetTx)(nil)

				cfg := &config.Internal{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &standardTxExecutor{
					backend: &Backend{
						Config:       cfg,
						Bootstrapped: utils.NewAtomic(true),
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					feeCalculator: feeCalculator,
					tx:            env.tx,
					state:         env.state,
				}
				return env.unsignedTx, e
			},
			err: platform.ErrNilTx,
		},
		{
			name: "max stake duration too large",
			newExecutor: func(ctrl *gomock.Controller) (*platform.TransformSubnetTx, *standardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				env.unsignedTx.MaxStakeDuration = math.MaxUint32

				cfg := &config.Internal{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &standardTxExecutor{
					backend: &Backend{
						Config:       cfg,
						Bootstrapped: utils.NewAtomic(true),
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					feeCalculator: feeCalculator,
					tx:            env.tx,
					state:         env.state,
				}
				return env.unsignedTx, e
			},
			err: errMaxStakeDurationTooLarge,
		},
		{
			name: "fail subnet authorization",
			newExecutor: func(ctrl *gomock.Controller) (*platform.TransformSubnetTx, *standardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				// Remove credentials
				env.tx.Creds = nil
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.SetSubnetOwner(env.unsignedTx.Subnet, subnetOwner)

				cfg := &config.Internal{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}

				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &standardTxExecutor{
					backend: &Backend{
						Config:       cfg,
						Bootstrapped: utils.NewAtomic(true),
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					feeCalculator: feeCalculator,
					tx:            env.tx,
					state:         env.state,
				}
				return env.unsignedTx, e
			},
			err: errWrongNumberOfCredentials,
		},
		{
			name: "flow checker failed",
			newExecutor: func(ctrl *gomock.Controller) (*platform.TransformSubnetTx, *standardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.SetSubnetOwner(env.unsignedTx.Subnet, subnetOwner)
				// GetSubnetToL1Conversion and GetSubnetTransformation return ErrNotFound by default.
				env.fx.EXPECT().VerifyPermission(gomock.Any(), env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil)
				env.flowChecker.EXPECT().VerifySpend(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(errFlowCheckFailed)

				cfg := &config.Internal{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}

				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &standardTxExecutor{
					backend: &Backend{
						Config:       cfg,
						Bootstrapped: utils.NewAtomic(true),
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					feeCalculator: feeCalculator,
					tx:            env.tx,
					state:         env.state,
				}
				return env.unsignedTx, e
			},
			err: errFlowCheckFailed,
		},
		{
			name: "invalid after subnet conversion",
			newExecutor: func(ctrl *gomock.Controller) (*platform.TransformSubnetTx, *standardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.SetSubnetOwner(env.unsignedTx.Subnet, subnetOwner)
				env.state.SetSubnetToL1Conversion(env.unsignedTx.Subnet, state.SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         make([]byte, 20),
				})
				env.fx.EXPECT().VerifyPermission(env.unsignedTx, env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil).Times(1)

				cfg := &config.Internal{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &standardTxExecutor{
					backend: &Backend{
						Config:       cfg,
						Bootstrapped: utils.NewAtomic(true),
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					feeCalculator: feeCalculator,
					tx:            env.tx,
					state:         env.state,
				}
				return env.unsignedTx, e
			},
			err: errIsImmutable,
		},
		{
			name: "valid tx",
			newExecutor: func(ctrl *gomock.Controller) (*platform.TransformSubnetTx, *standardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.SetSubnetOwner(env.unsignedTx.Subnet, subnetOwner)
				env.fx.EXPECT().VerifyPermission(env.unsignedTx, env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil).Times(1)
				env.flowChecker.EXPECT().VerifySpend(
					env.unsignedTx, env.state, env.unsignedTx.Ins, env.unsignedTx.Outs, env.tx.Creds[:len(env.tx.Creds)-1], gomock.Any(),
				).Return(nil).Times(1)

				cfg := &config.Internal{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}

				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &standardTxExecutor{
					backend: &Backend{
						Config:       cfg,
						Bootstrapped: utils.NewAtomic(true),
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					feeCalculator: feeCalculator,
					tx:            env.tx,
					state:         env.state,
				}
				return env.unsignedTx, e
			},
			stateAssertions: func(t *testing.T, state *state.Diff, stx *platform.Tx) {
				tx := stx.Unsigned.(*platform.TransformSubnetTx)

				// assert that the subnet's transform info was set
				gotTx, err := state.GetSubnetTransformation(tx.Subnet)
				require.NoError(t, err)
				require.Equal(t, stx, gotTx)

				// initial supply was updated
				supply, err := state.GetCurrentSupply(tx.Subnet)
				require.NoError(t, err)

				require.Equal(t, tx.InitialSupply, supply)

				// assert the utxos inputs were deleted
				for _, utxo := range tx.Ins {
					_, err := state.GetUTXO(utxo.InputID())
					require.ErrorIs(t, err, database.ErrNotFound)
				}

				// assert the utxos outputs were created
				for _, expectedOut := range stx.UTXOs() {
					utxo, err := state.GetUTXO(expectedOut.InputID())
					require.NoError(t, err)

					require.Equal(t, expectedOut.Out, utxo.Out)
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			unsignedTx, executor := tt.newExecutor(ctrl)
			err := executor.TransformSubnetTx(unsignedTx)
			require.ErrorIs(t, err, tt.err)

			if tt.stateAssertions != nil {
				tt.stateAssertions(t, executor.state, executor.tx)
			}
		})
	}
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

func TestStandardExecutorAddAutoRenewedValidatorTx(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Latest)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

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

	require.NotEmpty(t, inputIDs)
	for utxoID := range inputIDs {
		_, err := env.state.GetUTXO(utxoID)
		require.ErrorIs(t, err, database.ErrNotFound)
	}

	require.NotEmpty(t, baseTxOutputUTXOs)
	for _, wantUTXO := range baseTxOutputUTXOs {
		utxoID := wantUTXO.InputID()
		utxo, err := env.state.GetUTXO(utxoID)
		require.NoError(t, err)
		require.Equal(t, wantUTXO, utxo)
	}
}

func TestStandardExecutorAddAutoRenewedValidatorTxErrors(t *testing.T) {
	var (
		env           = newEnvironment(t, upgradetest.Latest)
		wallet        = newWallet(t, env, walletConfig{})
		feeCalculator = state.PickFeeCalculator(env.config, env.state)
	)

	tests := []struct {
		name   string
		want   error
		update func(*platform.AddAutoRenewedValidatorTx, *state.Diff)
	}{
		{
			name: "invalid_upgrade",
			update: func(_ *platform.AddAutoRenewedValidatorTx, diff *state.Diff) {
				diff.SetTimestamp(env.backend.Config.UpgradeConfig.HeliconTime.Add(-1 * time.Second))
			},
			want: errHeliconUpgradeNotActive,
		},
		{
			name: "weight_too_small",
			update: func(tx *platform.AddAutoRenewedValidatorTx, _ *state.Diff) {
				tx.StakeOuts[0].Out.(*secp256k1fx.TransferOutput).Amt = env.config.MinValidatorStake - 1
			},
			want: errWeightTooSmall,
		},
		{
			name: "weight_too_large",
			update: func(tx *platform.AddAutoRenewedValidatorTx, _ *state.Diff) {
				tx.StakeOuts[0].Out.(*secp256k1fx.TransferOutput).Amt = env.config.MaxValidatorStake + 1
			},
			want: errWeightTooLarge,
		},
		{
			name: "insufficient_delegation_fee",
			update: func(tx *platform.AddAutoRenewedValidatorTx, _ *state.Diff) {
				tx.DelegationShares = env.config.MinDelegationFee - 1
			},
			want: errInsufficientDelegationFee,
		},
		{
			name: "stake_too_short",
			update: func(tx *platform.AddAutoRenewedValidatorTx, _ *state.Diff) {
				tx.Period = uint64(env.config.HeliconMinStakeDuration.Seconds()) - 1
			},
			want: errStakeTooShort,
		},
		{
			name: "stake_too_long",
			update: func(tx *platform.AddAutoRenewedValidatorTx, _ *state.Diff) {
				tx.Period = uint64(env.config.MaxStakeDuration.Seconds()) + 1
			},
			want: ErrStakeTooLong,
		},
		{
			name: "duplicate_validator",
			update: func(tx *platform.AddAutoRenewedValidatorTx, _ *state.Diff) {
				tx.ValidatorNodeID = genesistest.DefaultNodeIDs[0].Bytes()
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

			tt.update(tx.Unsigned.(*platform.AddAutoRenewedValidatorTx), diff)

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

func TestStandardExecutorSetAutoRenewedValidatorConfigTx(t *testing.T) {
	const (
		delegationShares            = uint32(500_000)
		autoCompoundRewardShares    = uint32(300_000)
		newAutoCompoundRewardShares = uint32(300_000)
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
			require.NoError(t, diff.Apply(env.state))

			stakingInfo, err := env.state.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
			require.NoError(t, err)

			wantStakingInfo := initialStakingInfo
			wantStakingInfo.AutoCompoundRewardShares = newAutoCompoundRewardShares
			wantStakingInfo.NextPeriod = uint64(tt.newPeriod.Seconds())
			require.Equal(t, wantStakingInfo, stakingInfo)

			// After execution: the input UTXOs are consumed and the change outputs are created.
			for inputID := range inputIDs {
				_, err := env.state.GetUTXO(inputID)
				require.ErrorIs(t, err, database.ErrNotFound)
			}

			for _, wantUTXO := range outputUTXOs {
				gotUTXO, err := env.state.GetUTXO(wantUTXO.InputID())
				require.NoError(t, err)
				require.Equal(t, wantUTXO, gotUTXO)
			}
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
