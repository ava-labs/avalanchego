// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"math"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	db_manager "github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestUnlockUTXOs(t *testing.T) {
	fx := &secp256k1fx.Fx{}

	err := fx.InitializeVM(&secp256k1fx.TestVM{})
	require.NoError(t, err)

	err = fx.Bootstrapped()
	require.NoError(t, err)

	ctx := snow.DefaultContextTest()

	testHandler := &handler{
		ctx: ctx,
		clk: &mockable.Clock{},
		utxosReader: avax.NewUTXOState(
			memdb.New(),
			txs.Codec,
		),
		fx: fx,
	}

	cryptFactory := crypto.FactorySECP256K1R{}
	key, err := cryptFactory.NewPrivateKey()
	require.NoError(t, err)
	address := key.PublicKey().Address()
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{address},
	}
	existingTxID := ids.GenerateTestID()

	type want struct {
		ins  []*avax.TransferableInput
		outs []*avax.TransferableOutput
	}
	tests := map[string]struct {
		lockState     locked.State
		utxos         []*avax.UTXO
		generateWant  func([]*avax.UTXO) want
		expectedError error
	}{
		"Unbond bonded UTXOs": {
			lockState: locked.StateBonded,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, existingTxID),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], nil),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
					},
				}
			},
		},
		"Undeposit deposited UTXOs": {
			lockState: locked.StateDeposited,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 5, outputOwners, existingTxID, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], nil),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
					},
				}
			},
		},
		"Unbond deposited UTXOs": {
			lockState: locked.StateBonded,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 5, outputOwners, existingTxID, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins:  []*avax.TransferableInput{},
					outs: []*avax.TransferableOutput{},
				}
			},
		},
		"Undeposit bonded UTXOs": {
			lockState: locked.StateDeposited,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, existingTxID),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins:  []*avax.TransferableInput{},
					outs: []*avax.TransferableOutput{},
				}
			},
		},
		"Unlock unlocked UTXOs": {
			lockState: locked.StateBonded,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins:  []*avax.TransferableInput{},
					outs: []*avax.TransferableOutput{},
				}
			},
		},
		"Wrong state, lockStateUnlocked": {
			lockState:     locked.StateUnlocked,
			generateWant:  func(utxos []*avax.UTXO) want { return want{} },
			expectedError: errInvalidTargetLockState,
		},
		"Wrong state, LockStateDepositedBonded": {
			lockState:     locked.StateDepositedBonded,
			generateWant:  func(utxos []*avax.UTXO) want { return want{} },
			expectedError: errInvalidTargetLockState,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			expected := tt.generateWant(tt.utxos)
			ins, outs, err := testHandler.unlockUTXOs(tt.utxos, tt.lockState)

			require.Equal(expected.ins, ins)
			require.Equal(expected.outs, outs)
			require.ErrorIs(tt.expectedError, err)
		})
	}
}

func TestLock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	fx := &secp256k1fx.Fx{}

	err := fx.InitializeVM(&secp256k1fx.TestVM{})
	require.NoError(t, err)

	err = fx.Bootstrapped()
	require.NoError(t, err)

	config := defaultConfig()
	ctx := snow.DefaultContextTest()
	baseDBManager := db_manager.NewMemDB(version.Semantic1_0_0)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	rewardsCalc := reward.NewCalculator(config.RewardConfig)

	testState := defaultState(config, ctx, baseDB, rewardsCalc)

	cryptFactory := crypto.FactorySECP256K1R{}
	key, err := cryptFactory.NewPrivateKey()
	secpKey, ok := key.(*crypto.PrivateKeySECP256K1R)
	require.True(t, ok)
	require.NoError(t, err)
	address := key.PublicKey().Address()
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{address},
	}
	existingTxID := ids.GenerateTestID()

	type args struct {
		totalAmountToSpend uint64
		totalAmountToBurn  uint64
		appliedLockState   locked.State
	}
	type want struct {
		ins  []*avax.TransferableInput
		outs []*avax.TransferableOutput
	}
	tests := map[string]struct {
		utxos        []*avax.UTXO
		args         args
		generateWant func([]*avax.UTXO) want
		expectError  error
		msg          string
	}{
		"Happy path bonding": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateBonded,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 10, outputOwners, ids.Empty, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], []uint32{0}),
						generateTestInFromUTXO(utxos[1], []uint32{0}),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 9, outputOwners, ids.Empty, locked.ThisTxID),
						generateTestOut(ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
					},
				}
			},
			msg: "Happy path bonding",
		},
		"Happy path bonding deposited amount": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateBonded,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 10, outputOwners, existingTxID, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], []uint32{0}),
						generateTestInFromUTXO(utxos[1], []uint32{0}),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 9, outputOwners, existingTxID, locked.ThisTxID),
						generateTestOut(ctx.AVAXAssetID, 1, outputOwners, existingTxID, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 4, outputOwners, ids.Empty, ids.Empty),
					},
				}
			},
			msg: "Happy path bonding deposited amount",
		},
		"Bonding already bonded amount": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateBonded,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 10, outputOwners, ids.Empty, existingTxID),
			},
			expectError: errNotEnoughBalance,
			msg:         "Bonding already bonded amount",
		},
		"Not enough balance to bond": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateBonded,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
			},
			expectError: errNotEnoughBalance,
			msg:         "Not enough balance to bond",
		},
		"Happy path depositing": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateDeposited,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 10, outputOwners, ids.Empty, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], []uint32{0}),
						generateTestInFromUTXO(utxos[1], []uint32{0}),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 9, outputOwners, locked.ThisTxID, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
					},
				}
			},
			msg: "Happy path depositing",
		},
		"Happy path depositing bonded amount": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateDeposited,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 10, outputOwners, ids.Empty, existingTxID),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], []uint32{0}),
						generateTestInFromUTXO(utxos[1], []uint32{0}),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 9, outputOwners, locked.ThisTxID, existingTxID),
						generateTestOut(ctx.AVAXAssetID, 1, outputOwners, ids.Empty, existingTxID),
						generateTestOut(ctx.AVAXAssetID, 4, outputOwners, ids.Empty, ids.Empty),
					},
				}
			},
			msg: "Happy path depositing bonded amount",
		},
		"Depositing already deposited amount": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateDeposited,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 1, outputOwners, existingTxID, ids.Empty),
			},
			expectError: errNotEnoughBalance,
			msg:         "Depositing already deposited amount",
		},
		"Not enough balance to deposit": {
			args: args{
				totalAmountToSpend: 9,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateDeposited,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
			},
			expectError: errNotEnoughBalance,
			msg:         "Not enough balance to deposit",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			internalState := state.NewMockState(ctrl)
			utxoIDs := []ids.ID{}
			var want want
			var expectedSigners [][]*crypto.PrivateKeySECP256K1R
			if tt.expectError == nil {
				want = tt.generateWant(tt.utxos)
				expectedSigners = make([][]*crypto.PrivateKeySECP256K1R, len(want.ins))
				for i := range want.ins {
					expectedSigners[i] = []*crypto.PrivateKeySECP256K1R{secpKey}
				}
			}

			for _, utxo := range tt.utxos {
				testState.AddUTXO(utxo)
				utxoIDs = append(utxoIDs, utxo.InputID())
				internalState.EXPECT().GetUTXO(utxo.InputID()).Return(testState.GetUTXO(utxo.InputID()))
			}
			internalState.EXPECT().UTXOIDs(address.Bytes(), ids.Empty, math.MaxInt).Return(utxoIDs, nil)

			testHandler := &handler{
				ctx:         snow.DefaultContextTest(),
				clk:         &mockable.Clock{},
				utxosReader: internalState,
				fx:          fx,
			}

			ins, outs, signers, err := testHandler.Lock(
				[]*crypto.PrivateKeySECP256K1R{secpKey},
				tt.args.totalAmountToSpend,
				tt.args.totalAmountToBurn,
				address,
				tt.args.appliedLockState,
			)

			require.ErrorIs(err, tt.expectError, tt.msg)
			require.Equal(want.ins, ins)
			require.Equal(want.outs, outs)
			require.Equal(expectedSigners, signers)
		})
	}
}

func TestVerifyLockUTXOs(t *testing.T) {
	fx := &secp256k1fx.Fx{}

	err := fx.InitializeVM(&secp256k1fx.TestVM{})
	require.NoError(t, err)

	err = fx.Bootstrapped()
	require.NoError(t, err)

	testHandler := &handler{
		ctx: snow.DefaultContextTest(),
		clk: &mockable.Clock{},
		utxosReader: avax.NewUTXOState(
			memdb.New(),
			txs.Codec,
		),
		fx: fx,
	}
	assetID := testHandler.ctx.AVAXAssetID

	tx := &dummyUnsignedTx{txs.BaseTx{}}
	tx.Initialize([]byte{0})

	outputOwners1, cred1 := generateKeyAndSig(tx)
	outputOwners2, cred2 := generateKeyAndSig(tx)

	sigIndices := []uint32{0}
	existingTxID := ids.GenerateTestID()

	// Note that setting [chainTimestamp] also set's the VM's clock.
	// Adjust input/output locktimes accordingly.
	tests := map[string]struct {
		utxos            []*avax.UTXO
		ins              []*avax.TransferableInput
		outs             []*avax.TransferableOutput
		creds            []verify.Verifiable
		burnedAmount     uint64
		appliedLockState locked.State
		expectedErr      error
	}{
		"OK (no lock): produced + fee == consumed": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 10, outputOwners1, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, assetID, 10, outputOwners1, existingTxID, ids.Empty),
				generateTestUTXO(ids.ID{3}, assetID, 10, outputOwners2, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{4}, assetID, 10, outputOwners2, existingTxID, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 10, existingTxID, ids.Empty, sigIndices),
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 10, existingTxID, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{ // burn 1u:1, 2u:1
				generateTestOut(assetID, 9, outputOwners1, ids.Empty, ids.Empty),
				generateTestOut(assetID, 10, outputOwners1, existingTxID, ids.Empty),
				generateTestOut(assetID, 9, outputOwners2, ids.Empty, ids.Empty),
				generateTestOut(assetID, 10, outputOwners2, existingTxID, ids.Empty),
			},
			burnedAmount:     2,
			creds:            []verify.Verifiable{cred1, cred1, cred2, cred2},
			appliedLockState: locked.StateBonded,
			expectedErr:      nil,
		},
		"Fail (no lock): produced > consumed": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 1, outputOwners1, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, assetID, 2, outputOwners1, existingTxID, ids.Empty),
				generateTestUTXO(ids.ID{3}, assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{4}, assetID, 3, outputOwners2, existingTxID, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 1, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 2, existingTxID, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, existingTxID, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(assetID, 1, outputOwners1, ids.Empty, ids.Empty),
				generateTestOut(assetID, 2, outputOwners1, existingTxID, ids.Empty),
				generateTestOut(assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestOut(assetID, 4, outputOwners2, existingTxID, ids.Empty),
			},
			creds:            []verify.Verifiable{cred1, cred1, cred2, cred2},
			appliedLockState: locked.StateBonded,
			expectedErr:      errWrongProducedAmount,
		},
		"Fail (no lock): produced + fee > consumed": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 1, outputOwners1, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, assetID, 2, outputOwners1, existingTxID, ids.Empty),
				generateTestUTXO(ids.ID{3}, assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{4}, assetID, 3, outputOwners2, existingTxID, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 1, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 2, existingTxID, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, existingTxID, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{ // burn 1u:1, 2u:1
				generateTestOut(assetID, 1, outputOwners1, ids.Empty, ids.Empty),
				generateTestOut(assetID, 2, outputOwners1, existingTxID, ids.Empty),
				generateTestOut(assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestOut(assetID, 4, outputOwners2, existingTxID, ids.Empty),
			},
			burnedAmount:     2,
			creds:            []verify.Verifiable{cred1, cred1, cred2, cred2},
			appliedLockState: locked.StateBonded,
			expectedErr:      errWrongProducedAmount,
		},
		"OK (lock): produced + fee == consumed": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 10, outputOwners1, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, assetID, 10, outputOwners1, existingTxID, ids.Empty),
				generateTestUTXO(ids.ID{3}, assetID, 10, outputOwners2, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{4}, assetID, 10, outputOwners2, existingTxID, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 10, existingTxID, ids.Empty, sigIndices),
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 10, existingTxID, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{ // burn 1u:1, 2u:1; lock 1u:4, 1d:10, 2d: 5
				generateTestOut(assetID, 5, outputOwners1, ids.Empty, ids.Empty),
				generateTestOut(assetID, 4, outputOwners1, ids.Empty, locked.ThisTxID),
				generateTestOut(assetID, 10, outputOwners1, existingTxID, locked.ThisTxID),
				generateTestOut(assetID, 9, outputOwners2, ids.Empty, ids.Empty),
				generateTestOut(assetID, 5, outputOwners2, existingTxID, ids.Empty),
				generateTestOut(assetID, 5, outputOwners2, existingTxID, locked.ThisTxID),
			},
			burnedAmount:     2,
			creds:            []verify.Verifiable{cred1, cred1, cred2, cred2},
			appliedLockState: locked.StateBonded,
			expectedErr:      nil,
		},
		"Fail (lock): produced > consumed": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 2, outputOwners1, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, assetID, 2, outputOwners1, existingTxID, ids.Empty),
				generateTestUTXO(ids.ID{3}, assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{4}, assetID, 3, outputOwners2, existingTxID, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 2, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 2, existingTxID, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, existingTxID, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(assetID, 1, outputOwners1, ids.Empty, ids.Empty),
				generateTestOut(assetID, 1, outputOwners1, ids.Empty, locked.ThisTxID),
				generateTestOut(assetID, 2, outputOwners1, existingTxID, locked.ThisTxID),
				generateTestOut(assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestOut(assetID, 2, outputOwners2, existingTxID, ids.Empty),
				generateTestOut(assetID, 2, outputOwners2, existingTxID, locked.ThisTxID),
			},
			creds:            []verify.Verifiable{cred1, cred1, cred2, cred2},
			appliedLockState: locked.StateBonded,
			expectedErr:      errWrongProducedAmount,
		},
		"Fail (lock): produced + fee > consumed": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 1, outputOwners1, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, assetID, 2, outputOwners1, existingTxID, ids.Empty),
				generateTestUTXO(ids.ID{3}, assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{4}, assetID, 3, outputOwners2, existingTxID, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 1, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 2, existingTxID, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, ids.Empty, ids.Empty, sigIndices),
				generateTestIn(assetID, 3, existingTxID, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(assetID, 1, outputOwners1, ids.Empty, ids.Empty),
				generateTestOut(assetID, 2, outputOwners1, existingTxID, ids.Empty),
				generateTestOut(assetID, 3, outputOwners2, ids.Empty, ids.Empty),
				generateTestOut(assetID, 4, outputOwners2, existingTxID, ids.Empty),
			},
			burnedAmount:     2,
			creds:            []verify.Verifiable{cred1, cred1, cred2, cred2},
			appliedLockState: locked.StateBonded,
			expectedErr:      errWrongProducedAmount,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := testHandler.VerifyLockUTXOs(
				tx,
				test.utxos,
				test.ins,
				test.outs,
				test.creds,
				test.burnedAmount,
				assetID,
				test.appliedLockState,
			)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
