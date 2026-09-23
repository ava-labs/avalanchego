// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
)

func TestVerifyAddPermissionlessValidatorTx(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.PChainID)

	type test struct {
		name        string
		backendF    func() *Backend
		diff        *state.Diff
		sTxF        func() *platform.Tx
		txF         func() *platform.AddPermissionlessValidatorTx
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
		unsignedTransformTx = &platform.TransformSubnetTx{
			AssetID:           customAssetID,
			MinValidatorStake: 1,
			MaxValidatorStake: 2,
			MinStakeDuration:  3,
			MaxStakeDuration:  4,
			MinDelegationFee:  5,
			Subnet:            subnetID,
		}
		transformTx = platform.Tx{
			Unsigned: unsignedTransformTx,
			Creds:    []verify.Verifiable{},
		}
		// This tx already passed syntactic verification.
		startTime  = now.Add(time.Second)
		endTime    = startTime.Add(time.Second * time.Duration(unsignedTransformTx.MinStakeDuration))
		verifiedTx = platform.AddPermissionlessValidatorTx{
			BaseTx: platform.BaseTx{
				SyntacticallyVerified: true,
				BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
				},
			},
			Validator: platform.Validator{
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
			ValidatorRewardsOwner: newOwner(),
			DelegatorRewardsOwner: newOwner(),
			DelegationShares:      20_000,
		}
		verifiedSignedTx = platform.Tx{
			Unsigned: &verifiedTx,
			Creds:    []verify.Verifiable{},
		}
	)
	verifiedSignedTx.SetBytes([]byte{1}, []byte{2})

	tests := []test{
		{
			name: "fail syntactic verification",
			backendF: func() *Backend {
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
				}
			},

			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return nil
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: platform.ErrNilSignedTx,
		},
		{
			name: "not bootstrapped",
			backendF: func() *Backend {
				return &Backend{
					Ctx: ctx,
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Bootstrapped: &utils.Atomic[bool]{},
				}
			},
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				return &platform.AddPermissionlessValidatorTx{}
			},
			expectedErr: nil,
		},
		{
			name: "start time too early",
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(verifiedTx.StartTime())
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrTimestampNotBeforeStartTime,
		},
		{
			name: "weight too low",
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MinValidatorStake - 1
				return &tx
			},
			expectedErr: errWeightTooSmall,
		},
		{
			name: "weight too high",
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake + 1
				return &tx
			},
			expectedErr: errWeightTooLarge,
		},
		{
			name: "insufficient delegation fee",
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake
				tx.DelegationShares = unsignedTransformTx.MinDelegationFee - 1
				return &tx
			},
			expectedErr: errInsufficientDelegationFee,
		},
		{
			name: "duration too short",
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
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
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(time.Unix(1, 0))
				diff.AddSubnetTransformation(&transformTx)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
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
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
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
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)
				// State says validator exists
				primaryNetworkVdr := &state.Staker{
					EndTime:  mockable.MaxTime,
					SubnetID: constants.PrimaryNetworkID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, diff.PutCurrentValidator(primaryNetworkVdr))
				staker := &state.Staker{
					EndTime:  mockable.MaxTime,
					SubnetID: subnetID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, diff.PutCurrentValidator(staker))
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrDuplicateValidator,
		},
		{
			name: "validator not subset of primary network validator",
			backendF: func() *Backend {
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
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)

				// Validator time isn't subset of primary network validator time
				primaryNetworkVdr := &state.Staker{
					EndTime:  verifiedTx.EndTime().Add(-1 * time.Second),
					SubnetID: constants.PrimaryNetworkID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, diff.PutCurrentValidator(primaryNetworkVdr))
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: errPeriodMismatch,
		},
		{
			name: "success",
			backendF: func() *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, activeForkTime),
					},
					Ctx:          ctx,
					Bootstrapped: bootstrapped,
				}
			},
			diff: func() *state.Diff {
				diff, err := state.NewDiffOn(statetest.New(t, statetest.Config{}), state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				diff.SetTimestamp(now)
				diff.AddSubnetTransformation(&transformTx)
				primaryNetworkVdr := &state.Staker{
					EndTime:  mockable.MaxTime,
					SubnetID: constants.PrimaryNetworkID,
					NodeID:   verifiedTx.NodeID(),
				}
				require.NoError(t, diff.PutCurrentValidator(primaryNetworkVdr))
				return diff
			}(),
			sTxF: func() *platform.Tx {
				return &verifiedSignedTx
			},
			txF: func() *platform.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				backend = tt.backendF()
				sTx     = tt.sTxF()
				tx      = tt.txF()
			)

			err := verifyAddPermissionlessValidatorTx(backend, tt.diff, sTx, tx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
