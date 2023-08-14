// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	stdmath "math"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAddContinuousValidatorTxSyntacticVerify(t *testing.T) {
	type test struct {
		name   string
		txFunc func(*gomock.Controller) *AddContinuousValidatorTx
		err    error
	}

	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	ctx := &snow.Context{
		ChainID:   chainID,
		NetworkID: networkID,
	}

	// A BaseTx that already passed syntactic verification.
	verifiedBaseTx := BaseTx{
		SyntacticallyVerified: true,
	}

	// A BaseTx that passes syntactic verification.
	validBaseTx := BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	blsSK, err := bls.NewSecretKey()
	require.NoError(t, err)

	blsPOP := signer.NewProofOfPossession(blsSK)

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx: verifiedBaseTx,
				}
			},
			err: nil,
		},
		{
			name: "empty nodeID",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.EmptyNodeID,
					},
				}
			},
			err: errEmptyNodeID,
		},
		{
			name: "no provided stake",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
					},
					StakeOuts: nil,
				}
			},
			err: errNoStake,
		},
		{
			name: "too many delegation shares",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					DelegationShares: reward.PercentDenominator + 1,
				}
			},
			err: errTooManyDelegatorsShares,
		},
		{
			name: "too many reward restake shares",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardRestakeShares: reward.PercentDenominator + 1,
				}
			},
			err: errTooRestakeShares,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx: invalidBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					DelegationShares: reward.PercentDenominator,
				}
			},
			err: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid rewards owner",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(errCustom)
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					Signer: blsPOP,
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errCustom,
		},
		{
			name: "wrong signer",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					Signer: &signer.Empty{},
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errInvalidSigner,
		},
		{
			name: "wrong management key",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				wrongManagementKey := fx.NewMockOwner(ctrl)
				wrongManagementKey.EXPECT().Verify().Return(errCustom).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   2,
					},
					Signer:           blsPOP,
					ValidatorAuthKey: wrongManagementKey,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
				}
			},
			err: errCustom,
		},
		{
			name: "invalid stake output",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				stakeOut := avax.NewMockTransferableOut(ctrl)
				stakeOut.EXPECT().Verify().Return(errCustom)
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					Signer: blsPOP,
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: stakeOut,
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errCustom,
		},
		{
			name: "stake overflow",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					Signer: blsPOP,
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: stdmath.MaxUint64,
							},
						},
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 2,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: math.ErrOverflow,
		},
		{
			name: "multiple staked assets",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					Signer: blsPOP,
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errMultipleStakedAssets,
		},
		{
			name: "stake not sorted",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					Signer: blsPOP,
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 2,
							},
						},
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errOutputsNotSorted,
		},
		{
			name: "weight mismatch",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					Signer: blsPOP,
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errValidatorWeightMismatch,
		},
		{
			name: "valid primary network validator",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   2,
					},
					Signer: blsPOP,
					ValidatorAuthKey: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(ctx)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestAddContinuousValidatorTxNotDelegatorTx(t *testing.T) {
	txIntf := any((*AddContinuousValidatorTx)(nil))
	_, ok := txIntf.(DelegatorTx)
	require.False(t, ok)
}
