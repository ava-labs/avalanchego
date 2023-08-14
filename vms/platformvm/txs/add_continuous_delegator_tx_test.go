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
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAddContinuousDelegatorTxSyntacticVerify(t *testing.T) {
	type test struct {
		name   string
		txFunc func(*gomock.Controller) *AddContinuousDelegatorTx
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

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *AddContinuousDelegatorTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *AddContinuousDelegatorTx {
				return &AddContinuousDelegatorTx{
					BaseTx: verifiedBaseTx,
				}
			},
			err: nil,
		},
		{
			name: "wrong management key",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				wrongManagementKey := fx.NewMockOwner(ctrl)
				wrongManagementKey.EXPECT().Verify().Return(errCustom).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						Wght: 2,
					},
					DelegatorAuthKey: wrongManagementKey,
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
					DelegationRewardsOwner: rewardsOwner,
				}
			},
			err: errCustom,
		},
		{
			name: "no provided stake",
			txFunc: func(*gomock.Controller) *AddContinuousDelegatorTx {
				return &AddContinuousDelegatorTx{
					BaseTx:    validBaseTx,
					StakeOuts: nil,
				}
			},
			err: errNoStake,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(*gomock.Controller) *AddContinuousDelegatorTx {
				return &AddContinuousDelegatorTx{
					BaseTx: invalidBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
				}
			},
			err: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid rewards owner",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(errCustom)
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						Wght: 1,
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
					DelegationRewardsOwner: rewardsOwner,
				}
			},
			err: errCustom,
		},
		{
			name: "too many reward restake shares",
			txFunc: func(*gomock.Controller) *AddContinuousDelegatorTx {
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
					DelegatorRewardRestakeShares: reward.PercentDenominator + 1,
				}
			},
			err: errTooRestakeShares,
		},
		{
			name: "invalid stake output",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				stakeOut := avax.NewMockTransferableOut(ctrl)
				stakeOut.EXPECT().Verify().Return(errCustom)
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						Wght: 1,
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
					},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: ids.GenerateTestID(),
							},
							Out: stakeOut,
						},
					},
					DelegationRewardsOwner: rewardsOwner,
				}
			},
			err: errCustom,
		},
		{
			name: "multiple staked assets",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						Wght: 1,
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
					DelegationRewardsOwner: rewardsOwner,
				}
			},
			err: errMultipleStakedAssets,
		},
		{
			name: "stake not sorted",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						Wght: 1,
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
					DelegationRewardsOwner: rewardsOwner,
				}
			},
			err: errOutputsNotSorted,
		},
		{
			name: "stake overflow",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						Wght:   1,
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
					DelegationRewardsOwner: rewardsOwner,
				}
			},
			err: math.ErrOverflow,
		},
		{
			name: "weight mismatch",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						Wght: 1,
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
					DelegationRewardsOwner: rewardsOwner,
				}
			},
			err: errDelegatorWeightMismatch,
		},
		{
			name: "valid primary network validator",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousDelegatorTx {
				rewardsOwner := fx.NewMockOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()
				assetID := ids.GenerateTestID()
				return &AddContinuousDelegatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						Wght: 2,
					},
					DelegatorAuthKey: &secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						Threshold: 1,
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
					DelegationRewardsOwner: rewardsOwner,
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

func TestAddContinuousDelegatorTxNotValidatorTx(t *testing.T) {
	txIntf := any((*AddContinuousDelegatorTx)(nil))
	_, ok := txIntf.(ValidatorTx)
	require.False(t, ok)
}
