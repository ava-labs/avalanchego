// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo/utxomock"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestVerifyAddPermissionlessValidatorTx(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.PChainID)

	type test struct {
		name        string
		backendF    func(*gomock.Controller) *Backend
		chain       state.Chain
		sTxF        func() *txs.Tx
		txF         func() *txs.AddPermissionlessValidatorTx
		expectedErr error
	}

	var (
		// in the following tests we set the fork time for forks we want active
		// to activeForkTime, which is ensured to be before any other time related
		// quantity (based on now)
		activeForkTime = time.Unix(0, 0)
		now            = time.Now().Truncate(time.Second) // after activeForkTime

		subnetID            = ids.GenerateTestID()
		customAssetID       = ids.GenerateTestID()
		unsignedTransformTx = &txs.TransformSubnetTx{
			AssetID:           customAssetID,
			MinValidatorStake: 1,
			MaxValidatorStake: 2,
			MinStakeDuration:  3,
			MaxStakeDuration:  4,
			MinDelegationFee:  5,
			Subnet:            subnetID,
		}
		transformTx = txs.Tx{
			Unsigned: unsignedTransformTx,
			Creds:    []verify.Verifiable{},
		}
		// This tx already passed syntactic verification.
		startTime  = now.Add(time.Second)
		endTime    = startTime.Add(time.Second * time.Duration(unsignedTransformTx.MinStakeDuration))
		verifiedTx = txs.AddPermissionlessValidatorTx{
			BaseTx: txs.BaseTx{
				SyntacticallyVerified: true,
				BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
				},
			},
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				// Note: [Start] is not set here as it will be ignored
				// Post-Durango in favor of the current chain time
				End:  uint64(endTime.Unix()),
				Wght: unsignedTransformTx.MinValidatorStake,
			},
			Subnet: subnetID,
			StakeOuts: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{
						ID: customAssetID,
					},
				},
			},
			ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				Threshold: 1,
			},
			DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				Threshold: 1,
			},
			DelegationShares: 20_000,
		}
		verifiedSignedTx = txs.Tx{
			Unsigned: &verifiedTx,
			Creds:    []verify.Verifiable{},
		}
	)
	verifiedSignedTx.SetBytes([]byte{1}, []byte{2})

	tests := []test{
		{
			name: "fail syntactic verification",
			backendF: func(*gomock.Controller) *Backend {
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
				}
			},

			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return nil
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return nil
			},
			expectedErr: txs.ErrNilSignedTx,
		},
		{
			name: "not bootstrapped",
			backendF: func(*gomock.Controller) *Backend {
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: &utils.Atomic[bool]{},
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &txs.AddPermissionlessValidatorTx{}
			},
			expectedErr: nil,
		},
		{
			name: "start time too early",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Cortina, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(verifiedTx.StartTime())
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrTimestampNotBeforeStartTime,
		},
		{
			name: "weight too low",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MinValidatorStake - 1
				return &tx
			},
			expectedErr: errWeightTooSmall,
		},
		{
			name: "weight too high",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake + 1
				return &tx
			},
			expectedErr: errWeightTooLarge,
		},
		{
			name: "insufficient delegation fee",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake
				tx.DelegationShares = unsignedTransformTx.MinDelegationFee - 1
				return &tx
			},
			expectedErr: errInsufficientDelegationFee,
		},
		{
			name: "duration too short",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake
				tx.DelegationShares = unsignedTransformTx.MinDelegationFee

				// Note the duration is 1 less than the minimum
				tx.Validator.End = tx.Validator.Start + uint64(unsignedTransformTx.MinStakeDuration) - 1
				return &tx
			},
			expectedErr: errStakeTooShort,
		},
		{
			name: "duration too long",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(time.Unix(1, 0))
				s.AddSubnetTransformation(&transformTx)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake
				tx.DelegationShares = unsignedTransformTx.MinDelegationFee

				// Note the duration is more than the maximum
				tx.Validator.End = uint64(unsignedTransformTx.MaxStakeDuration) + 2
				return &tx
			},
			expectedErr: ErrStakeTooLong,
		},
		{
			name: "wrong assetID",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.StakeOuts = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
					},
				}
				return &tx
			},
			expectedErr: errWrongStakedAssetID,
		},
		{
			name: "duplicate validator",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)
				// State says validator exists
				staker := &state.Staker{
					EndTime:  mockable.MaxTime,
					SubnetID: subnetID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, s.PutCurrentValidator(staker))
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrDuplicateValidator,
		},
		{
			name: "validator not subset of primary network validator",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)

				// Validator time isn't subset of primary network validator time
				primaryNetworkVdr := &state.Staker{
					EndTime:  verifiedTx.EndTime().Add(-1 * time.Second),
					SubnetID: constants.PrimaryNetworkID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, s.PutCurrentValidator(primaryNetworkVdr))
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: errPeriodMismatch,
		},
		{
			name: "flow check fails",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxomock.NewVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(errFlowCheckFailed)

				return &Backend{
					FlowChecker: flowChecker,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Ctx:          ctx,
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)

				primaryNetworkVdr := &state.Staker{
					EndTime:  mockable.MaxTime,
					SubnetID: constants.PrimaryNetworkID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, s.PutCurrentValidator(primaryNetworkVdr))
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: errFlowCheckFailed,
		},
		{
			name: "success",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxomock.NewVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(nil)

				return &Backend{
					FlowChecker: flowChecker,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Ctx:          ctx,
					Bootstrapped: bootstrapped,
				}
			},
			chain: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				s.SetTimestamp(now)
				s.AddSubnetTransformation(&transformTx)
				primaryNetworkVdr := &state.Staker{
					EndTime:  mockable.MaxTime,
					SubnetID: constants.PrimaryNetworkID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, s.PutCurrentValidator(primaryNetworkVdr))
				return s
			}(),
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			var (
				backend = tt.backendF(ctrl)
				sTx     = tt.sTxF()
				tx      = tt.txF()
			)

			feeCalculator := state.PickFeeCalculator(backend.Config, tt.chain)
			err := verifyAddPermissionlessValidatorTx(backend, feeCalculator, tt.chain, sTx, tx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
