// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/avax/avaxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestAddAutoRenewedValidatorTxSyntacticVerify(t *testing.T) {
	dummyErr := errors.New("dummy error")

	type test struct {
		name    string
		txFunc  func(*gomock.Controller) *AddAutoRenewedValidatorTx
		wantErr error
	}

	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	avaxAssetID := ids.GenerateTestID()
	ctx := &snow.Context{
		ChainID:     chainID,
		NetworkID:   networkID,
		AVAXAssetID: avaxAssetID,
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

	blsSK, err := localsigner.New()
	require.NoError(t, err)

	blsPOP, err := signer.NewProofOfPossession(blsSK)
	require.NoError(t, err)

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *AddAutoRenewedValidatorTx {
				return nil
			},
			wantErr: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *AddAutoRenewedValidatorTx {
				return &AddAutoRenewedValidatorTx{
					BaseTx: verifiedBaseTx,
				}
			},
			wantErr: nil,
		},
		{
			name: "empty nodeID",
			txFunc: func(*gomock.Controller) *AddAutoRenewedValidatorTx {
				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.EmptyNodeID,
				}
			},
			wantErr: errEmptyNodeID,
		},
		{
			name: "no provided stake",
			txFunc: func(*gomock.Controller) *AddAutoRenewedValidatorTx {
				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					StakeOuts:       nil,
				}
			},
			wantErr: errNoStake,
		},
		{
			name: "missing period",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					DelegationShares: reward.PercentDenominator,
					Owner:            configOwner,
				}
			},
			wantErr: errMissingPeriod,
		},
		{
			name: "too many shares",
			txFunc: func(*gomock.Controller) *AddAutoRenewedValidatorTx {
				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					DelegationShares: reward.PercentDenominator + 1,
				}
			},
			wantErr: errTooManyShares,
		},
		{
			name: "too many auto compound reward shares",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					DelegationShares:         reward.PercentDenominator,
					AutoCompoundRewardShares: reward.PercentDenominator + 1,
					Owner:                    configOwner,
				}
			},
			wantErr: errTooManyAutoCompoundRewardShares,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          invalidBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					DelegationShares: reward.PercentDenominator,
					Owner:            configOwner,
				}
			},
			wantErr: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid validator rewards owner",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				invalidRewardsOwner := fxmock.NewOwner(ctrl)
				invalidRewardsOwner.EXPECT().Verify().Return(dummyErr)

				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).MaxTimes(1)

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          &signer.Empty{},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: invalidRewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: dummyErr,
		},
		{
			name: "invalid delegator rewards owner",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				invalidRewardsOwner := fxmock.NewOwner(ctrl)
				invalidRewardsOwner.EXPECT().Verify().Return(dummyErr)

				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil)

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          &signer.Empty{},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: invalidRewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: dummyErr,
		},
		{
			name: "wrong signer",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()
				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          &signer.Empty{},
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: errMissingSigner,
		},
		{
			name: "invalid stake output",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				stakeOut := avaxmock.NewTransferableOut(ctrl)
				stakeOut.EXPECT().Verify().Return(dummyErr)

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: stakeOut,
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: dummyErr,
		},
		{
			name: "stake overflow",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: math.MaxUint64,
							},
						},
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 2,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name: "invalid staked asset",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
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
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: errInvalidStakedAsset,
		},
		{
			name: "stake not sorted",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 2,
							},
						},
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: errOutputsNotSorted,
		},
		{
			name: "weight mismatch",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					Owner:                 configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			wantErr: errValidatorWeightMismatch,
		},
		{
			name: "valid auto-renewed validator",
			txFunc: func(ctrl *gomock.Controller) *AddAutoRenewedValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddAutoRenewedValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            2,
					Signer:          blsPOP,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					DelegationShares:      reward.PercentDenominator,
					Owner:                 configOwner,
				}
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			tx := tt.txFunc(ctrl)
			gotErr := tx.SyntacticVerify(ctx)
			require.ErrorIs(t, gotErr, tt.wantErr)

			if tx != nil {
				require.Equal(t, tt.wantErr == nil, tx.SyntacticallyVerified)
			}
		})
	}
}
