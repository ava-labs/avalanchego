// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/avax/avaxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAddContinuousValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)

	dummyErr := errors.New("dummy error")

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

	blsSK, err := localsigner.New()
	require.NoError(err)

	blsPOP, err := signer.NewProofOfPossession(blsSK)
	require.NoError(err)

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
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.EmptyNodeID,
				}
			},
			err: errEmptyNodeID,
		},
		{
			name: "no provided stake",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					StakeOuts:       nil,
				}
			},
			err: errNoStake,
		},
		{
			name: "missing period",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
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
					ConfigOwner:      configOwner,
				}
			},
			err: errMissingPeriod,
		},
		{
			name: "missing config owner",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
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
					Period:           1,
				}
			},
			err: errMissingConfigOwner,
		},
		{
			name: "too many shares",
			txFunc: func(*gomock.Controller) *AddContinuousValidatorTx {
				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
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
			err: errTooManyDelegationShares,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddContinuousValidatorTx{
					BaseTx:          invalidBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
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
					ConfigOwner:      configOwner,
				}
			},
			err: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid validator rewards owner",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				invalidRewardsOwner := fxmock.NewOwner(ctrl)
				invalidRewardsOwner.EXPECT().Verify().Return(dummyErr)

				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).MaxTimes(1)

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          &signer.Empty{},
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
					ValidatorRewardsOwner: invalidRewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: dummyErr,
		},
		{
			name: "invalid delegator rewards owner",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				invalidRewardsOwner := fxmock.NewOwner(ctrl)
				invalidRewardsOwner.EXPECT().Verify().Return(dummyErr)

				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil)

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          &signer.Empty{},
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
					DelegatorRewardsOwner: invalidRewardsOwner,
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: dummyErr,
		},
		{
			name: "wrong signer",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()
				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          &signer.Empty{},
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
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errMissingSigner,
		},
		{
			name: "invalid stake output",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				stakeOut := avaxmock.NewTransferableOut(ctrl)
				stakeOut.EXPECT().Verify().Return(dummyErr)

				return &AddContinuousValidatorTx{
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
							Out: stakeOut,
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: dummyErr,
		},
		{
			name: "stake overflow",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				assetID := ids.GenerateTestID()
				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
					StakeOuts: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: math.MaxUint64,
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
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: safemath.ErrOverflow,
		},
		{
			name: "multiple staked assets",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				return &AddContinuousValidatorTx{
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
								ID: ids.GenerateTestID(),
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
							},
						},
					},
					ValidatorRewardsOwner: rewardsOwner,
					DelegatorRewardsOwner: rewardsOwner,
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errMultipleStakedAssets,
		},
		{
			name: "stake not sorted",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				assetID := ids.GenerateTestID()

				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
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
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errOutputsNotSorted,
		},
		{
			name: "weight mismatch",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				assetID := ids.GenerateTestID()
				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            1,
					Signer:          blsPOP,
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
					ConfigOwner:           configOwner,
					DelegationShares:      reward.PercentDenominator,
				}
			},
			err: errValidatorWeightMismatch,
		},
		{
			name: "valid continuous validator",
			txFunc: func(ctrl *gomock.Controller) *AddContinuousValidatorTx {
				rewardsOwner := fxmock.NewOwner(ctrl)
				rewardsOwner.EXPECT().Verify().Return(nil).AnyTimes()

				configOwner := fxmock.NewOwner(ctrl)
				configOwner.EXPECT().Verify().Return(nil).AnyTimes()

				assetID := ids.GenerateTestID()

				return &AddContinuousValidatorTx{
					BaseTx:          validBaseTx,
					ValidatorNodeID: ids.GenerateTestNodeID(),
					Period:          1,
					Wght:            2,
					Signer:          blsPOP,
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
					ConfigOwner:           configOwner,
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(ctx)
			require.ErrorIs(err, tt.err)

			if tx != nil {
				require.Equal(tt.err == nil, tx.SyntacticallyVerified)
			}
		})
	}
}

func TestAddContinuousValidatorIsValidatorTx(t *testing.T) {
	require := require.New(t)

	txIntf := any((*AddContinuousValidatorTx)(nil))
	_, ok := txIntf.(ValidatorTx)
	require.True(ok)
}

func TestAddContinuousValidatorIsContinuousStaker(t *testing.T) {
	require := require.New(t)

	txIntf := any((*AddContinuousValidatorTx)(nil))
	_, ok := txIntf.(ContinuousStaker)
	require.True(ok)
}
