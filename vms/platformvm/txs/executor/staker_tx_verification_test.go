// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// time.Duration underlying type is currently int64
const stakerMaxDuration time.Duration = math.MaxInt64

var errCustom = errors.New("custom")

func TestVerifyAddContinuousValidatorTx(t *testing.T) {
	type test struct {
		name        string
		backendF    func(*gomock.Controller) *Backend
		stateF      func(*gomock.Controller) state.Chain
		sTxF        func() *txs.Tx
		txF         func() *txs.AddContinuousValidatorTx
		expectedErr error
	}

	var (
		primaryNetworkCfg = config.Config{
			ContinuousStakingTime: time.Time{}, // activate latest fork
			MinValidatorStake:     1,
			MaxValidatorStake:     2,
			MinStakeDuration:      3 * time.Second,
			MaxStakeDuration:      4 * time.Second,
			MinDelegationFee:      5,
		}
		// This tx already passed syntactic verification.
		dummyTime  = time.Now().Truncate(time.Second)
		verifiedTx = txs.AddContinuousValidatorTx{
			BaseTx: txs.BaseTx{
				SyntacticallyVerified: true,
				BaseTx: avax.BaseTx{
					NetworkID:    1,
					BlockchainID: ids.GenerateTestID(),
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
				},
			},
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(dummyTime.Unix()),
				End:    uint64(dummyTime.Add(primaryNetworkCfg.MinStakeDuration).Unix()),
				Wght:   primaryNetworkCfg.MinValidatorStake,
			},
			// No BLS key not Auth one, since they are verified by syntax verification
			StakeOuts: []*avax.TransferableOutput{
				{},
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
					Ctx:    snow.DefaultContextTest(),
					Config: &primaryNetworkCfg,
				}
			},
			stateF: func(*gomock.Controller) state.Chain {
				return nil
			},
			sTxF: func() *txs.Tx {
				return nil
			},
			txF: func() *txs.AddContinuousValidatorTx {
				return nil
			},
			expectedErr: txs.ErrNilSignedTx,
		},
		{
			name: "not bootstrapped",
			backendF: func(*gomock.Controller) *Backend {
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: &utils.Atomic[bool]{},
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				return &verifiedTx
			},
			expectedErr: nil,
		},
		{
			name: "tx not accepted pre continuous fork",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				cfg := primaryNetworkCfg
				cfg.CortinaTime = time.Time{}
				cfg.ContinuousStakingTime = mockable.MaxTime

				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &cfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(verifiedTx.StartTime())
				return state
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrTxUnacceptableBeforeFork,
		},
		{
			name: "weight too low",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = primaryNetworkCfg.MinValidatorStake - 1
				return &tx
			},
			expectedErr: ErrWeightTooSmall,
		},
		{
			name: "weight too high",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = primaryNetworkCfg.MaxValidatorStake + 1
				return &tx
			},
			expectedErr: ErrWeightTooLarge,
		},
		{
			name: "insufficient delegation fee",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = primaryNetworkCfg.MaxValidatorStake
				tx.DelegationShares = primaryNetworkCfg.MinDelegationFee - 1
				return &tx
			},
			expectedErr: ErrInsufficientDelegationFee,
		},
		{
			name: "duration too short",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				// Note the duration is 1 less than the minimum
				tx.Validator.Start = uint64(dummyTime.Add(time.Second).Unix())
				tx.Validator.End = uint64(dummyTime.Add(primaryNetworkCfg.MinStakeDuration).Unix())
				return &tx
			},
			expectedErr: ErrStakeTooShort,
		},
		{
			name: "duration too long",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				// Note the duration is more than the maximum
				tx.Validator.Start = uint64(dummyTime.Unix())
				tx.Validator.End = uint64(dummyTime.Add(time.Second).Add(primaryNetworkCfg.MaxStakeDuration).Unix())
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
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
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
			expectedErr: ErrWrongStakedAssetID,
		},
		{
			name: "duplicate validator",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				// State says validator exists
				s.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrDuplicateValidator,
		},
		{
			name: "failed checking for existing validator",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				// State says validator exists
				s.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, errCustom)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				return &verifiedTx
			},
			expectedErr: errCustom,
		},
		{
			name: "flow check fails",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(ErrFlowCheckFailed)

				return &Backend{
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				mockState.EXPECT().GetPendingValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrFlowCheckFailed,
		},
		{
			name: "success",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(nil)

				return &Backend{
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				mockState.EXPECT().GetPendingValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousValidatorTx {
				return &verifiedTx
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var (
				backend = tt.backendF(ctrl)
				state   = tt.stateF(ctrl)
				sTx     = tt.sTxF()
				tx      = tt.txF()
			)

			err := verifyAddContinuousValidatorTx(backend, state, sTx, tx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestVerifyStopContinuousValidatorTx(t *testing.T) {
	type test struct {
		name                    string
		backendF                func(*gomock.Controller) *Backend
		stateF                  func(*gomock.Controller) state.Chain
		sTxF                    func() *txs.Tx
		txF                     func() *txs.StopStakerTx
		expectedStoppingStakers []*state.Staker
		expectedStopTime        time.Time
		expectedErr             error
	}

	blsSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	primaryNetworkCfg := config.Config{
		ContinuousStakingTime: time.Time{}, // activate latest fork
		MinValidatorStake:     1,
		MaxValidatorStake:     2,
		MinStakeDuration:      3 * time.Second,
		MaxStakeDuration:      4 * time.Second,
		MinDelegationFee:      5,
	}

	addr := preFundedKeys[0].Address()

	// primary validator tx to retrieve ValidatorAuthKey from
	primaryValidatorTx := &txs.AddContinuousValidatorTx{
		ValidatorAuthKey: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
	}
	signedPrimaryValidatorTx := txs.Tx{
		Unsigned: primaryValidatorTx,
		Creds:    []verify.Verifiable{},
	}
	signedPrimaryValidatorTx.SetBytes([]byte{1}, []byte{2})

	var (
		chainTime        = time.Now().Truncate(time.Second)
		primaryValidator = &state.Staker{
			TxID:      signedPrimaryValidatorTx.ID(),
			NodeID:    ids.GenerateTestNodeID(),
			PublicKey: blsPOP.Key(),
			SubnetID:  constants.PlatformChainID,
			Weight:    primaryNetworkCfg.MinValidatorStake,

			StartTime:       chainTime,
			StakingPeriod:   primaryNetworkCfg.MinStakeDuration,
			EndTime:         mockable.MaxTime,
			PotentialReward: uint64(0), // not relevant for this test
			NextTime:        chainTime.Add(primaryNetworkCfg.MinStakeDuration),
			Priority:        txs.PrimaryNetworkContinuousValidatorCurrentPriority,
		}

		// This tx already passed syntactic verification.
		verifiedTx = txs.StopStakerTx{
			BaseTx: txs.BaseTx{
				SyntacticallyVerified: true,
				BaseTx: avax.BaseTx{
					NetworkID:    1,
					BlockchainID: ids.GenerateTestID(),
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
				},
			},
			TxID: primaryValidator.TxID,
			StakerAuth: &secp256k1fx.Input{
				SigIndices: []uint32{0},
			},
		}
		verifiedSignedTx = txs.Tx{
			Unsigned: &verifiedTx,
			Creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: [][secp256k1.SignatureLen]byte{{1, 3, 3, 7}},
				},
			},
		}
	)
	verifiedSignedTx.SetBytes([]byte{3}, []byte{4})

	tests := []test{
		{
			name: "success",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				fxMock := fx.NewMockFx(ctrl)
				fxMock.EXPECT().VerifyPermission(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(nil)

				return &Backend{
					Fx:           fxMock,
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// first round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryValidator)
				currentStakerIter.EXPECT().Release()

				// second round to find all stakers to stop. Bare minimum mocks to pass
				currentStakerIter.EXPECT().Next().Return(false)
				currentStakerIter.EXPECT().Release()

				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryValidatorTx.ID()).Return(&signedPrimaryValidatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedStoppingStakers: []*state.Staker{primaryValidator},
			expectedStopTime:        primaryValidator.NextTime,
			expectedErr:             nil,
		},
		{
			name: "fail syntactic verification",
			backendF: func(*gomock.Controller) *Backend {
				return &Backend{
					Ctx:    snow.DefaultContextTest(),
					Config: &primaryNetworkCfg,
				}
			},
			stateF: func(*gomock.Controller) state.Chain {
				return nil
			},
			sTxF: func() *txs.Tx {
				return nil
			},
			txF: func() *txs.StopStakerTx {
				return nil
			},
			expectedErr: txs.ErrNilSignedTx,
		},
		{
			name: "not bootstrapped",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(false)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// first round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryValidator)
				currentStakerIter.EXPECT().Release()

				// second round to find all stakers to stop. Bare minimum mocks to pass
				currentStakerIter.EXPECT().Next().Return(false)
				currentStakerIter.EXPECT().Release()

				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedStoppingStakers: []*state.Staker{primaryValidator},
			expectedStopTime:        primaryValidator.NextTime,
			expectedErr:             nil,
		},
		{
			name: "tx not accepted pre continuous fork",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				cfg := primaryNetworkCfg
				cfg.CortinaTime = time.Time{}
				cfg.ContinuousStakingTime = mockable.MaxTime

				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &cfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrTxUnacceptableBeforeFork,
		},
		{
			name: "failed getting staker iterator",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetCurrentStakerIterator().Return(nil, errCustom)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: errCustom,
		},
		{
			name: "no staker to stop",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))

				currentStakerIter := state.NewMockStakerIterator(ctrl)
				currentStakerIter.EXPECT().Next().Return(false)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrNoStakerToStop,
		},
		{
			name: "failed staker stop verification",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				fxMock := fx.NewMockFx(ctrl)
				fxMock.EXPECT().VerifyPermission(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errCustom)

				return &Backend{
					Fx:           fxMock,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// first round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryValidator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryValidatorTx.ID()).Return(&signedPrimaryValidatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrUnauthorizedStakerStopping,
		},
		{
			name: "missing validator tx",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// first round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryValidator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryValidatorTx.ID()).Return(nil, status.Unknown, errCustom)

				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: errCustom,
		},
		{
			name: "can't stop non-continuous validator",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryValidator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				nonContinuousValidatorTx := txs.Tx{
					Unsigned: &txs.AddValidatorTx{},
				}
				mockState.EXPECT().GetTx(signedPrimaryValidatorTx.ID()).Return(&nonContinuousValidatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrUnauthorizedStakerStopping,
		},
		{
			name: "failed flow check",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				fxMock := fx.NewMockFx(ctrl)
				fxMock.EXPECT().VerifyPermission(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(errCustom)

				return &Backend{
					Fx:           fxMock,
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// first round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryValidator)
				currentStakerIter.EXPECT().Release()

				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryValidatorTx.ID()).Return(&signedPrimaryValidatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var (
				backend = tt.backendF(ctrl)
				state   = tt.stateF(ctrl)
				sTx     = tt.sTxF()
				tx      = tt.txF()
			)

			stoppingStakers, stoppingTime, err := verifyStopStakerTx(backend, state, sTx, tx)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedStoppingStakers, stoppingStakers)
			require.Equal(t, tt.expectedStopTime, stoppingTime)
		})
	}
}

func TestVerifyAddContinuousDelegatorTx(t *testing.T) {
	type test struct {
		name     string
		backendF func(*gomock.Controller) *Backend
		stateF   func(*gomock.Controller) state.Chain
		sTxF     func() *txs.Tx
		txF      func() *txs.AddContinuousDelegatorTx

		expectedValTime            time.Time
		expectedEndTime            time.Time
		expectedMinStakingDuration time.Duration
		expectedErr                error
	}

	blsSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	var (
		primaryNetworkCfg = config.Config{
			ContinuousStakingTime: time.Time{}, // activate latest fork
			MinValidatorStake:     1,
			MinDelegatorStake:     1,
			MaxValidatorStake:     2,
			MinStakeDuration:      1 * time.Second,
			MaxStakeDuration:      100 * time.Second,
			MinDelegationFee:      5,
		}

		chainTime        = time.Now().Truncate(time.Second)
		delegatorPeriod  = primaryNetworkCfg.MinStakeDuration
		validatorPeriod  = delegatorPeriod * 16
		primaryValidator = &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    ids.GenerateTestNodeID(),
			PublicKey: blsPOP.Key(),
			SubnetID:  constants.PlatformChainID,
			Weight:    primaryNetworkCfg.MinValidatorStake,

			StartTime:       chainTime,
			StakingPeriod:   validatorPeriod,
			EndTime:         mockable.MaxTime,
			PotentialReward: uint64(0), // not relevant for this test
			NextTime:        chainTime.Add(validatorPeriod),
			Priority:        txs.PrimaryNetworkContinuousValidatorCurrentPriority,
		}

		// This tx already passed syntactic verification.
		dummyTime  = time.Now().Truncate(time.Second)
		verifiedTx = txs.AddContinuousDelegatorTx{
			BaseTx: txs.BaseTx{
				SyntacticallyVerified: true,
				BaseTx: avax.BaseTx{
					NetworkID:    1,
					BlockchainID: ids.GenerateTestID(),
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
				},
			},
			Validator: txs.Validator{
				NodeID: primaryValidator.NodeID,
				Start:  uint64(dummyTime.Unix()),
				End:    uint64(dummyTime.Add(delegatorPeriod).Unix()),
				Wght:   primaryNetworkCfg.MinValidatorStake,
			},
			StakeOuts: []*avax.TransferableOutput{
				{},
			},
			DelegationRewardsOwner: &secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				Threshold: 1,
			},
			DelegatorRewardRestakeShares: 20_000,
		}
		verifiedSignedTx = txs.Tx{
			Unsigned: &verifiedTx,
			Creds:    []verify.Verifiable{},
		}
	)
	verifiedSignedTx.SetBytes([]byte{1}, []byte{2})

	tests := []test{
		{
			name: "success",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(nil)

				return &Backend{
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(chainTime)

				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(primaryValidator, nil)

				currentDelegatorIter := state.NewMockStakerIterator(ctrl)
				currentDelegatorIter.EXPECT().Next().Return(false).AnyTimes()
				currentDelegatorIter.EXPECT().Release().AnyTimes()
				mockState.EXPECT().GetCurrentDelegatorIterator(
					primaryValidator.SubnetID,
					primaryValidator.NodeID,
				).Return(currentDelegatorIter, nil).AnyTimes()

				pendingDelegatorIter := state.NewMockStakerIterator(ctrl)
				pendingDelegatorIter.EXPECT().Next().Return(false).AnyTimes()
				pendingDelegatorIter.EXPECT().Release().AnyTimes()
				mockState.EXPECT().GetPendingDelegatorIterator(
					primaryValidator.SubnetID,
					primaryValidator.NodeID,
				).Return(currentDelegatorIter, nil).AnyTimes()

				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedValTime:            primaryValidator.StartTime,
			expectedEndTime:            mockable.MaxTime,
			expectedMinStakingDuration: primaryNetworkCfg.MinStakeDuration,
			expectedErr:                nil,
		},
		{
			name: "fail syntactic verification",
			backendF: func(*gomock.Controller) *Backend {
				return &Backend{
					Ctx:    snow.DefaultContextTest(),
					Config: &primaryNetworkCfg,
				}
			},
			stateF: func(*gomock.Controller) state.Chain {
				return nil
			},
			sTxF: func() *txs.Tx {
				return nil
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return nil
			},
			expectedErr: txs.ErrNilSignedTx,
		},
		{
			name: "not bootstrapped",
			backendF: func(*gomock.Controller) *Backend {
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: &utils.Atomic[bool]{},
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(chainTime)
				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(primaryValidator, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedValTime:            primaryValidator.StartTime,
			expectedEndTime:            mockable.MaxTime,
			expectedMinStakingDuration: primaryNetworkCfg.MinStakeDuration,
			expectedErr:                nil,
		},
		{
			name: "tx not accepted pre continuous fork",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				cfg := primaryNetworkCfg
				cfg.CortinaTime = time.Time{}
				cfg.ContinuousStakingTime = mockable.MaxTime

				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &cfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(verifiedTx.StartTime())
				return state
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedErr: ErrTxUnacceptableBeforeFork,
		},
		{
			name: "weight too low",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(chainTime)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = primaryNetworkCfg.MinDelegatorStake - 1
				return &tx
			},
			expectedErr: ErrWeightTooSmall,
		},
		{
			name: "duration too short",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				// Note the duration is 1 less than the minimum
				tx.Validator.Start = uint64(dummyTime.Add(time.Second).Unix())
				tx.Validator.End = uint64(dummyTime.Add(primaryNetworkCfg.MinStakeDuration).Unix())
				return &tx
			},
			expectedErr: ErrStakeTooShort,
		},
		{
			name: "duration too long",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				// Note the duration is more than the maximum
				tx.Validator.Start = uint64(dummyTime.Unix())
				tx.Validator.End = uint64(dummyTime.Add(time.Second).Add(primaryNetworkCfg.MaxStakeDuration).Unix())
				return &tx
			},
			expectedErr: ErrStakeTooLong,
		},
		{
			name: "duration not divisor of validator period",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(primaryValidator, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				// pick delegator period that is *not* divisor of validator period
				wrongDelegatorPeriod := delegatorPeriod + 2*time.Second
				require.True(t, validatorPeriod%wrongDelegatorPeriod != 0)

				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Start = uint64(dummyTime.Unix())
				tx.Validator.End = uint64(dummyTime.Add(wrongDelegatorPeriod).Unix())
				return &tx
			},
			expectedErr: ErrPeriodMismatch,
		},
		{
			name: "duration not power of two divisor of validator period",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(primaryValidator, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				// pick delegator period that is *not* power of two divisor of validator period
				wrongDelegatorPeriod := validatorPeriod / 5
				require.True(t, validatorPeriod%wrongDelegatorPeriod == 0)
				require.False(t, safemath.IsPowerOfTwo(int(validatorPeriod/wrongDelegatorPeriod)))

				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Start = uint64(dummyTime.Unix())
				tx.Validator.End = uint64(dummyTime.Add(wrongDelegatorPeriod).Unix())
				return &tx
			},
			expectedErr: ErrPeriodMismatch,
		},
		{
			name: "wrong assetID",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
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
			expectedErr: ErrWrongStakedAssetID,
		},
		{
			name: "missing current validator",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				// State says validator does not exists
				s.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedErr: database.ErrNotFound,
		},
		{
			name: "validator is pending",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))

				pendValidator := *primaryValidator
				pendValidator.Priority = txs.PrimaryNetworkValidatorPendingPriority
				s.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(&pendValidator, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedErr: ErrContinuousDelegatorToNonContinuousValidator,
		},
		{
			name: "validator is not continuous",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &primaryNetworkCfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))

				pendValidator := *primaryValidator
				pendValidator.Priority = txs.PrimaryNetworkValidatorCurrentPriority
				s.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(&pendValidator, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedErr: ErrContinuousDelegatorToNonContinuousValidator,
		},
		{
			name: "delegator staking period larger than validator one",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(chainTime)

				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(primaryValidator, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				tx := verifiedTx
				sTx := txs.Tx{
					Unsigned: &tx,
					Creds:    []verify.Verifiable{},
				}
				sTx.SetBytes([]byte{1}, []byte{2})

				excessiveStakingPeriod := primaryValidator.StakingPeriod + time.Second
				sTx.Unsigned.(*txs.AddContinuousDelegatorTx).Validator.Start = uint64(dummyTime.Unix())
				sTx.Unsigned.(*txs.AddContinuousDelegatorTx).Validator.End = uint64(dummyTime.Add(excessiveStakingPeriod).Unix())
				return &sTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				tx := verifiedTx
				excessiveStakingPeriod := primaryValidator.StakingPeriod + time.Second
				tx.Validator.Start = uint64(dummyTime.Unix())
				tx.Validator.End = uint64(dummyTime.Add(excessiveStakingPeriod).Unix())
				return &tx
			},
			expectedErr: ErrPeriodMismatch,
		},
		{
			name: "delegator wouls break validator max weight",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(chainTime)

				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(primaryValidator, nil)

				// mock just enough to simulate that validator has already the maximum
				// amount of delegation it is allowed to
				currentDelegatorIter := state.NewMockStakerIterator(ctrl)
				currentDelegatorIter.EXPECT().Next().Return(true)
				currentDelegatorIter.EXPECT().Value().Return(&state.Staker{
					Weight: primaryNetworkCfg.MaxValidatorStake * MaxValidatorWeightFactor,
				})
				currentDelegatorIter.EXPECT().Next().Return(false).AnyTimes()
				currentDelegatorIter.EXPECT().Release().AnyTimes()
				mockState.EXPECT().GetCurrentDelegatorIterator(
					primaryValidator.SubnetID,
					primaryValidator.NodeID,
				).Return(currentDelegatorIter, nil).AnyTimes()

				pendingDelegatorIter := state.NewMockStakerIterator(ctrl)
				pendingDelegatorIter.EXPECT().Next().Return(false).AnyTimes()
				pendingDelegatorIter.EXPECT().Release().AnyTimes()
				mockState.EXPECT().GetPendingDelegatorIterator(
					primaryValidator.SubnetID,
					primaryValidator.NodeID,
				).Return(currentDelegatorIter, nil).AnyTimes()

				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedErr: ErrOverDelegated,
		},
		{
			name: "flow check fails",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(ErrFlowCheckFailed)

				return &Backend{
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(chainTime)

				mockState.EXPECT().GetCurrentValidator(verifiedTx.SubnetID(), verifiedTx.NodeID()).Return(primaryValidator, nil)

				currentDelegatorIter := state.NewMockStakerIterator(ctrl)
				currentDelegatorIter.EXPECT().Next().Return(false).AnyTimes()
				currentDelegatorIter.EXPECT().Release().AnyTimes()
				mockState.EXPECT().GetCurrentDelegatorIterator(
					primaryValidator.SubnetID,
					primaryValidator.NodeID,
				).Return(currentDelegatorIter, nil).AnyTimes()

				pendingDelegatorIter := state.NewMockStakerIterator(ctrl)
				pendingDelegatorIter.EXPECT().Next().Return(false).AnyTimes()
				pendingDelegatorIter.EXPECT().Release().AnyTimes()
				mockState.EXPECT().GetPendingDelegatorIterator(
					primaryValidator.SubnetID,
					primaryValidator.NodeID,
				).Return(currentDelegatorIter, nil).AnyTimes()

				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddContinuousDelegatorTx {
				return &verifiedTx
			},
			expectedErr: ErrFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var (
				backend = tt.backendF(ctrl)
				state   = tt.stateF(ctrl)
				sTx     = tt.sTxF()
				tx      = tt.txF()
			)

			valTime, endTime, minStakingDuration, err := verifyAddContinuousDelegatorTx(backend, state, sTx, tx)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedValTime, valTime)
			require.Equal(t, tt.expectedEndTime, endTime)
			require.Equal(t, tt.expectedMinStakingDuration, minStakingDuration)
		})
	}
}

func TestVerifyStopContinuousDelegatorTx(t *testing.T) {
	type test struct {
		name                    string
		backendF                func(*gomock.Controller) *Backend
		stateF                  func(*gomock.Controller) state.Chain
		sTxF                    func() *txs.Tx
		txF                     func() *txs.StopStakerTx
		expectedStoppingStakers []*state.Staker
		expectedStopTime        time.Time
		expectedErr             error
	}

	primaryNetworkCfg := config.Config{
		ContinuousStakingTime: time.Time{}, // activate latest fork
		MinValidatorStake:     1,
		MaxValidatorStake:     2,
		MinDelegatorStake:     1,
		MinStakeDuration:      3 * time.Second,
		MaxStakeDuration:      4 * time.Second,
		MinDelegationFee:      5,
	}

	addr := preFundedKeys[0].Address()

	// primary delegator tx to retrieve DelegatorAuthKey from
	primaryDelegatorTx := &txs.AddContinuousDelegatorTx{
		DelegatorAuthKey: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
	}
	signedPrimaryDelegatorTx := txs.Tx{
		Unsigned: primaryDelegatorTx,
		Creds:    []verify.Verifiable{},
	}
	signedPrimaryDelegatorTx.SetBytes([]byte{1}, []byte{2})

	var (
		chainTime        = time.Now().Truncate(time.Second)
		primaryDelegator = &state.Staker{
			TxID:     signedPrimaryDelegatorTx.ID(),
			NodeID:   ids.GenerateTestNodeID(),
			SubnetID: constants.PlatformChainID,
			Weight:   primaryNetworkCfg.MinDelegatorStake,

			StartTime:       chainTime,
			StakingPeriod:   primaryNetworkCfg.MinStakeDuration,
			EndTime:         mockable.MaxTime,
			PotentialReward: uint64(0), // not relevant for this test
			NextTime:        chainTime.Add(primaryNetworkCfg.MinStakeDuration),
			Priority:        txs.PrimaryNetworkContinuousDelegatorCurrentPriority,
		}

		// This tx already passed syntactic verification.
		verifiedTx = txs.StopStakerTx{
			BaseTx: txs.BaseTx{
				SyntacticallyVerified: true,
				BaseTx: avax.BaseTx{
					NetworkID:    1,
					BlockchainID: ids.GenerateTestID(),
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
				},
			},
			TxID: primaryDelegator.TxID,
			StakerAuth: &secp256k1fx.Input{
				SigIndices: []uint32{0},
			},
		}
		verifiedSignedTx = txs.Tx{
			Unsigned: &verifiedTx,
			Creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: [][secp256k1.SignatureLen]byte{{1, 3, 3, 7}},
				},
			},
		}
	)
	verifiedSignedTx.SetBytes([]byte{3}, []byte{4})

	tests := []test{
		{
			name: "success",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				fxMock := fx.NewMockFx(ctrl)
				fxMock.EXPECT().VerifyPermission(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(nil)

				return &Backend{
					Fx:           fxMock,
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryDelegator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryDelegatorTx.ID()).Return(&signedPrimaryDelegatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedStoppingStakers: []*state.Staker{primaryDelegator},
			expectedStopTime:        primaryDelegator.NextTime,
			expectedErr:             nil,
		},
		{
			name: "fail syntactic verification",
			backendF: func(*gomock.Controller) *Backend {
				return &Backend{
					Ctx:    snow.DefaultContextTest(),
					Config: &primaryNetworkCfg,
				}
			},
			stateF: func(*gomock.Controller) state.Chain {
				return nil
			},
			sTxF: func() *txs.Tx {
				return nil
			},
			txF: func() *txs.StopStakerTx {
				return nil
			},
			expectedErr: txs.ErrNilSignedTx,
		},
		{
			name: "not bootstrapped",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(false)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryDelegator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedStoppingStakers: []*state.Staker{primaryDelegator},
			expectedStopTime:        primaryDelegator.NextTime,
			expectedErr:             nil,
		},
		{
			name: "tx not accepted pre continuous fork",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				cfg := primaryNetworkCfg
				cfg.CortinaTime = time.Time{}
				cfg.ContinuousStakingTime = mockable.MaxTime

				return &Backend{
					Ctx:          snow.DefaultContextTest(),
					Config:       &cfg,
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrTxUnacceptableBeforeFork,
		},
		{
			name: "failed getting staker iterator",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetCurrentStakerIterator().Return(nil, errCustom)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: errCustom,
		},
		{
			name: "no staker to stop",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))

				currentStakerIter := state.NewMockStakerIterator(ctrl)
				currentStakerIter.EXPECT().Next().Return(false)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrNoStakerToStop,
		},
		{
			name: "failed staker stop verification",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				fxMock := fx.NewMockFx(ctrl)
				fxMock.EXPECT().VerifyPermission(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errCustom)

				return &Backend{
					Fx:           fxMock,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryDelegator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryDelegatorTx.ID()).Return(&signedPrimaryDelegatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrUnauthorizedStakerStopping,
		},
		{
			name: "missing delegator tx",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryDelegator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryDelegatorTx.ID()).Return(nil, status.Unknown, errCustom)

				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: errCustom,
		},
		{
			name: "can't stop non-continuous delegator",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				return &Backend{
					Fx:           nil,
					FlowChecker:  nil,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryDelegator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				nonContinuousDelegatorTx := txs.Tx{
					Unsigned: &txs.AddDelegatorTx{},
				}
				mockState.EXPECT().GetTx(signedPrimaryDelegatorTx.ID()).Return(&nonContinuousDelegatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrUnauthorizedStakerStopping,
		},
		{
			name: "failed flow check",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				fxMock := fx.NewMockFx(ctrl)
				fxMock.EXPECT().VerifyPermission(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(errCustom)

				return &Backend{
					Fx:           fxMock,
					FlowChecker:  flowChecker,
					Config:       &primaryNetworkCfg,
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				// round to find staker to stop
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(primaryDelegator)
				currentStakerIter.EXPECT().Release()
				mockState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()

				mockState.EXPECT().GetTx(signedPrimaryDelegatorTx.ID()).Return(&signedPrimaryDelegatorTx, status.Committed, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.StopStakerTx {
				return &verifiedTx
			},
			expectedErr: ErrFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var (
				backend = tt.backendF(ctrl)
				state   = tt.stateF(ctrl)
				sTx     = tt.sTxF()
				tx      = tt.txF()
			)

			stoppingStakers, stoppingTime, err := verifyStopStakerTx(backend, state, sTx, tx)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedStoppingStakers, stoppingStakers)
			require.Equal(t, tt.expectedStopTime, stoppingTime)
		})
	}
}

func TestVerifyAddPermissionlessValidatorTx(t *testing.T) {
	type test struct {
		name        string
		backendF    func(*gomock.Controller) *Backend
		stateF      func(*gomock.Controller) state.Chain
		sTxF        func() *txs.Tx
		txF         func() *txs.AddPermissionlessValidatorTx
		expectedErr error
	}

	var (
		subnetID            = ids.GenerateTestID()
		customAssetID       = ids.GenerateTestID()
		unsignedTransformTx = &txs.TransformSubnetTx{
			AssetID:           customAssetID,
			MinValidatorStake: 1,
			MaxValidatorStake: 2,
			MinStakeDuration:  3,
			MaxStakeDuration:  4,
			MinDelegationFee:  5,
		}
		transformTx = txs.Tx{
			Unsigned: unsignedTransformTx,
			Creds:    []verify.Verifiable{},
		}
		// This tx already passed syntactic verification.
		verifiedTx = txs.AddPermissionlessValidatorTx{
			BaseTx: txs.BaseTx{
				SyntacticallyVerified: true,
				BaseTx: avax.BaseTx{
					NetworkID:    1,
					BlockchainID: ids.GenerateTestID(),
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
				},
			},
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  1,
				End:    1 + uint64(unsignedTransformTx.MinStakeDuration),
				Wght:   unsignedTransformTx.MinValidatorStake,
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
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
				}
			},
			stateF: func(*gomock.Controller) state.Chain {
				return nil
			},
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
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: &utils.Atomic[bool]{},
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: nil,
		},
		{
			name: "start time too early",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						CortinaTime:           time.Time{},
						ContinuousStakingTime: mockable.MaxTime,
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(verifiedTx.StartTime())
				return state
			},
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
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				s.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MinValidatorStake - 1
				return &tx
			},
			expectedErr: ErrWeightTooSmall,
		},
		{
			name: "weight too high",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				s.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake + 1
				return &tx
			},
			expectedErr: ErrWeightTooLarge,
		},
		{
			name: "insufficient delegation fee",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				s.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake
				tx.DelegationShares = unsignedTransformTx.MinDelegationFee - 1
				return &tx
			},
			expectedErr: ErrInsufficientDelegationFee,
		},
		{
			name: "duration too short",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				s.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake
				tx.DelegationShares = unsignedTransformTx.MinDelegationFee
				// Note the duration is 1 less than the minimum
				tx.Validator.Start = 1
				tx.Validator.End = uint64(unsignedTransformTx.MinStakeDuration)
				return &tx
			},
			expectedErr: ErrStakeTooShort,
		},
		{
			name: "duration too long",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				s.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				return s
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				tx := verifiedTx // Note that this copies [verifiedTx]
				tx.Validator.Wght = unsignedTransformTx.MaxValidatorStake
				tx.DelegationShares = unsignedTransformTx.MinDelegationFee
				// Note the duration is more than the maximum
				tx.Validator.Start = 1
				tx.Validator.End = 2 + uint64(unsignedTransformTx.MaxStakeDuration)
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
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				s.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				return s
			},
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
			expectedErr: ErrWrongStakedAssetID,
		},
		{
			name: "duplicate validator",
			backendF: func(*gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)
				return &Backend{
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				s.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				// State says validator exists
				s.EXPECT().GetCurrentValidator(subnetID, verifiedTx.NodeID()).Return(nil, nil)
				return s
			},
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
					Ctx: snow.DefaultContextTest(),
					Config: &config.Config{
						ContinuousStakingTime: time.Time{}, // activate latest fork
					},
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				mockState.EXPECT().GetCurrentValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				mockState.EXPECT().GetPendingValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				// Validator time isn't subset of primary network validator time
				primaryNetworkVdr := &state.Staker{
					StartTime:     verifiedTx.StartTime().Add(time.Second),
					StakingPeriod: verifiedTx.StakingPeriod() - 1,
					EndTime:       verifiedTx.EndTime(),
				}
				mockState.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, verifiedTx.NodeID()).Return(primaryNetworkVdr, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrPeriodMismatch,
		},
		{
			name: "flow check fails",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxo.NewMockVerifier(ctrl)
				flowChecker.EXPECT().VerifySpend(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(ErrFlowCheckFailed)

				return &Backend{
					FlowChecker: flowChecker,
					Config: &config.Config{
						AddSubnetValidatorFee: 1,
						ContinuousStakingTime: time.Time{}, // activate latest fork,
					},
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				mockState.EXPECT().GetCurrentValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				mockState.EXPECT().GetPendingValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				primaryNetworkVdr := &state.Staker{
					StartTime:     verifiedTx.StartTime().Add(-1 * time.Second),
					StakingPeriod: verifiedTx.StakingPeriod(),
					EndTime:       verifiedTx.EndTime(),
				}
				mockState.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, verifiedTx.NodeID()).Return(primaryNetworkVdr, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				return &verifiedTx
			},
			expectedErr: ErrFlowCheckFailed,
		},
		{
			name: "starts too far in the future",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxo.NewMockVerifier(ctrl)
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
					Config: &config.Config{
						CortinaTime:           time.Time{},
						ContinuousStakingTime: mockable.MaxTime,
						AddSubnetValidatorFee: 1,
					},
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				mockState.EXPECT().GetCurrentValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				mockState.EXPECT().GetPendingValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				primaryNetworkVdr := &state.Staker{
					StartTime: time.Unix(0, 0),
					EndTime:   mockable.MaxTime,
				}
				mockState.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, verifiedTx.NodeID()).Return(primaryNetworkVdr, nil)
				return mockState
			},
			sTxF: func() *txs.Tx {
				return &verifiedSignedTx
			},
			txF: func() *txs.AddPermissionlessValidatorTx {
				// Note this copies [verifiedTx]
				tx := verifiedTx
				tx.Validator.Start = uint64(MaxFutureStartTime.Seconds()) + 1
				tx.Validator.End = tx.Validator.Start + uint64(unsignedTransformTx.MinStakeDuration)
				return &tx
			},
			expectedErr: ErrFutureStakeTime,
		},
		{
			name: "success",
			backendF: func(ctrl *gomock.Controller) *Backend {
				bootstrapped := &utils.Atomic[bool]{}
				bootstrapped.Set(true)

				flowChecker := utxo.NewMockVerifier(ctrl)
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
					Config: &config.Config{
						AddSubnetValidatorFee: 1,
						ContinuousStakingTime: time.Time{}, // activate latest fork,
					},
					Ctx:          snow.DefaultContextTest(),
					Bootstrapped: bootstrapped,
				}
			},
			stateF: func(ctrl *gomock.Controller) state.Chain {
				mockState := state.NewMockChain(ctrl)
				mockState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				mockState.EXPECT().GetSubnetTransformation(subnetID).Return(&transformTx, nil)
				mockState.EXPECT().GetCurrentValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				mockState.EXPECT().GetPendingValidator(subnetID, verifiedTx.NodeID()).Return(nil, database.ErrNotFound)
				primaryNetworkVdr := &state.Staker{
					StartTime:     time.Unix(0, 0),
					StakingPeriod: stakerMaxDuration,
					EndTime:       mockable.MaxTime,
				}
				mockState.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, verifiedTx.NodeID()).Return(primaryNetworkVdr, nil)
				return mockState
			},
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
				state   = tt.stateF(ctrl)
				sTx     = tt.sTxF()
				tx      = tt.txF()
			)

			err := verifyAddPermissionlessValidatorTx(backend, state, sTx, tx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestGetValidatorRules(t *testing.T) {
	type test struct {
		name          string
		subnetID      ids.ID
		backend       *Backend
		chainStateF   func(*gomock.Controller) state.Chain
		expectedRules *addValidatorRules
		expectedErr   error
	}

	var (
		config = &config.Config{
			MinValidatorStake: 1,
			MaxValidatorStake: 2,
			MinStakeDuration:  time.Second,
			MaxStakeDuration:  2 * time.Second,
			MinDelegationFee:  1337,
		}
		avaxAssetID   = ids.GenerateTestID()
		customAssetID = ids.GenerateTestID()
		subnetID      = ids.GenerateTestID()
	)

	tests := []test{
		{
			name:     "primary network",
			subnetID: constants.PrimaryNetworkID,
			backend: &Backend{
				Config: config,
				Ctx: &snow.Context{
					AVAXAssetID: avaxAssetID,
				},
			},
			chainStateF: func(*gomock.Controller) state.Chain {
				return nil
			},
			expectedRules: &addValidatorRules{
				assetID:           avaxAssetID,
				minValidatorStake: config.MinValidatorStake,
				maxValidatorStake: config.MaxValidatorStake,
				minStakeDuration:  config.MinStakeDuration,
				maxStakeDuration:  config.MaxStakeDuration,
				minDelegationFee:  config.MinDelegationFee,
			},
		},
		{
			name:     "can't get subnet transformation",
			subnetID: subnetID,
			backend:  nil,
			chainStateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetSubnetTransformation(subnetID).Return(nil, errTest)
				return state
			},
			expectedRules: &addValidatorRules{},
			expectedErr:   errTest,
		},
		{
			name:     "invalid transformation tx",
			subnetID: subnetID,
			backend:  nil,
			chainStateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				tx := &txs.Tx{
					Unsigned: &txs.AddDelegatorTx{},
				}
				state.EXPECT().GetSubnetTransformation(subnetID).Return(tx, nil)
				return state
			},
			expectedRules: &addValidatorRules{},
			expectedErr:   ErrIsNotTransformSubnetTx,
		},
		{
			name:     "subnet",
			subnetID: subnetID,
			backend:  nil,
			chainStateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				tx := &txs.Tx{
					Unsigned: &txs.TransformSubnetTx{
						AssetID:           customAssetID,
						MinValidatorStake: config.MinValidatorStake,
						MaxValidatorStake: config.MaxValidatorStake,
						MinStakeDuration:  1337,
						MaxStakeDuration:  42,
						MinDelegationFee:  config.MinDelegationFee,
					},
				}
				state.EXPECT().GetSubnetTransformation(subnetID).Return(tx, nil)
				return state
			},
			expectedRules: &addValidatorRules{
				assetID:           customAssetID,
				minValidatorStake: config.MinValidatorStake,
				maxValidatorStake: config.MaxValidatorStake,
				minStakeDuration:  1337 * time.Second,
				maxStakeDuration:  42 * time.Second,
				minDelegationFee:  config.MinDelegationFee,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chainState := tt.chainStateF(ctrl)
			rules, err := getValidatorRules(tt.backend, chainState, tt.subnetID)
			if tt.expectedErr != nil {
				require.ErrorIs(err, tt.expectedErr)
				return
			}
			require.NoError(err)
			require.Equal(tt.expectedRules, rules)
		})
	}
}

func TestGetDelegatorRules(t *testing.T) {
	type test struct {
		name          string
		subnetID      ids.ID
		backend       *Backend
		chainStateF   func(*gomock.Controller) state.Chain
		expectedRules *addDelegatorRules
		expectedErr   error
	}
	var (
		config = &config.Config{
			MinDelegatorStake: 1,
			MaxValidatorStake: 2,
			MinStakeDuration:  time.Second,
			MaxStakeDuration:  2 * time.Second,
		}
		avaxAssetID   = ids.GenerateTestID()
		customAssetID = ids.GenerateTestID()
		subnetID      = ids.GenerateTestID()
	)
	tests := []test{
		{
			name:     "primary network",
			subnetID: constants.PrimaryNetworkID,
			backend: &Backend{
				Config: config,
				Ctx: &snow.Context{
					AVAXAssetID: avaxAssetID,
				},
			},
			chainStateF: func(*gomock.Controller) state.Chain {
				return nil
			},
			expectedRules: &addDelegatorRules{
				assetID:                  avaxAssetID,
				minDelegatorStake:        config.MinDelegatorStake,
				maxValidatorStake:        config.MaxValidatorStake,
				minStakeDuration:         config.MinStakeDuration,
				maxStakeDuration:         config.MaxStakeDuration,
				maxValidatorWeightFactor: MaxValidatorWeightFactor,
			},
		},
		{
			name:     "can't get subnet transformation",
			subnetID: subnetID,
			backend:  nil,
			chainStateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetSubnetTransformation(subnetID).Return(nil, errTest)
				return state
			},
			expectedRules: &addDelegatorRules{},
			expectedErr:   errTest,
		},
		{
			name:     "invalid transformation tx",
			subnetID: subnetID,
			backend:  nil,
			chainStateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				tx := &txs.Tx{
					Unsigned: &txs.AddDelegatorTx{},
				}
				state.EXPECT().GetSubnetTransformation(subnetID).Return(tx, nil)
				return state
			},
			expectedRules: &addDelegatorRules{},
			expectedErr:   ErrIsNotTransformSubnetTx,
		},
		{
			name:     "subnet",
			subnetID: subnetID,
			backend:  nil,
			chainStateF: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				tx := &txs.Tx{
					Unsigned: &txs.TransformSubnetTx{
						AssetID:                  customAssetID,
						MinDelegatorStake:        config.MinDelegatorStake,
						MinValidatorStake:        config.MinValidatorStake,
						MaxValidatorStake:        config.MaxValidatorStake,
						MinStakeDuration:         1337,
						MaxStakeDuration:         42,
						MinDelegationFee:         config.MinDelegationFee,
						MaxValidatorWeightFactor: 21,
					},
				}
				state.EXPECT().GetSubnetTransformation(subnetID).Return(tx, nil)
				return state
			},
			expectedRules: &addDelegatorRules{
				assetID:                  customAssetID,
				minDelegatorStake:        config.MinDelegatorStake,
				maxValidatorStake:        config.MaxValidatorStake,
				minStakeDuration:         1337 * time.Second,
				maxStakeDuration:         42 * time.Second,
				maxValidatorWeightFactor: 21,
			},
			expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chainState := tt.chainStateF(ctrl)
			rules, err := getDelegatorRules(tt.backend, chainState, tt.subnetID)
			if tt.expectedErr != nil {
				require.ErrorIs(err, tt.expectedErr)
				return
			}
			require.NoError(err)
			require.Equal(tt.expectedRules, rules)
		})
	}
}
