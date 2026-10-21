// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo/utxomock"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

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

// Ensure Execute fails when there are not enough control sigs
func TestCreateChainTxInsufficientControlSigs(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	// Remove a signature
	tx.Creds[0].(*secp256k1fx.Credential).Sigs = tx.Creds[0].(*secp256k1fx.Credential).Sigs[1:]

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, errUnauthorizedModification)
}

// Ensure Execute fails when an incorrect control signature is given
func TestCreateChainTxWrongControlSig(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	// Generate new, random key to sign tx with
	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Replace a valid signature with one from another key
	sig, err := key.SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
	require.NoError(err)
	copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, errUnauthorizedModification)
}

// Ensure Execute fails when the Subnet the blockchain specifies as
// its validator set doesn't exist
func TestCreateChainTxNoSuchSubnet(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	tx.Unsigned.(*platform.CreateChainTx).SubnetID = ids.GenerateTestID()

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	builderDiff, err := state.NewDiffOn(stateDiff, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, builderDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, database.ErrNotFound)
}

// Ensure valid tx passes semanticVerify
func TestCreateChainTxValid(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	builderDiff, err := state.NewDiffOn(stateDiff, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, builderDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.NoError(err)
}

func TestCreateChainTxAP3FeeChange(t *testing.T) {
	ap3Time := genesistest.DefaultValidatorStartTime.Add(time.Hour)
	tests := []struct {
		name          string
		time          time.Time
		fee           uint64
		expectedError error
	}{
		{
			name:          "pre-fork - correctly priced",
			time:          genesistest.DefaultValidatorStartTime,
			fee:           0,
			expectedError: nil,
		},
		{
			name:          "post-fork - correctly priced",
			time:          ap3Time,
			fee:           100 * defaultTxFee,
			expectedError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.Banff)
			env.config.UpgradeConfig.ApricotPhase3Time = ap3Time

			addrs := set.NewSet[ids.ShortID](len(genesistest.DefaultFundedKeys))
			for _, key := range genesistest.DefaultFundedKeys {
				addrs.Add(key.Address())
			}

			env.state.SetTimestamp(test.time) // to duly set fee

			config := *env.config
			subnetID := testSubnet1.ID()
			wallet := newWallet(t, env, walletConfig{
				config:    &config,
				subnetIDs: []ids.ID{subnetID},
			})

			tx, err := wallet.IssueCreateChainTx(
				subnetID,
				nil,
				ids.GenerateTestID(),
				nil,
				"",
			)
			require.NoError(err)

			stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			stateDiff.SetTimestamp(test.time)

			feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				stateDiff,
			)
			require.ErrorIs(err, test.expectedError)
		})
	}
}

func TestEtnaCreateChainTxInvalidWithManagedSubnet(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Etna)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	builderDiff, err := state.NewDiffOn(stateDiff, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	stateDiff.SetSubnetToL1Conversion(
		subnetID,
		state.SubnetToL1Conversion{
			ConversionID: ids.GenerateTestID(),
			ChainID:      ids.GenerateTestID(),
			Addr:         []byte("address"),
		},
	)

	feeCalculator := state.PickFeeCalculator(env.config, builderDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, errIsImmutable)
}
