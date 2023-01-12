// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	db_manager "github.com/ava-labs/avalanchego/database/manager"
)

func TestUnlockUTXOs(t *testing.T) {
	testHandler := defaultCaminoHandler(t, nil)
	ctx := testHandler.ctx

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
			lockState: locked.StateUnlocked,
			generateWant: func(utxos []*avax.UTXO) want {
				return want{}
			},
			expectedError: errInvalidTargetLockState,
		},
		"Wrong state, LockStateDepositedBonded": {
			lockState: locked.StateDepositedBonded,
			generateWant: func(utxos []*avax.UTXO) want {
				return want{}
			},
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

	testKeys := crypto.BuildTestKeys()
	changeOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{testKeys[0].Address()},
	}
	recipientOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{testKeys[1].Address()},
	}

	existingTxID := ids.GenerateTestID()

	type args struct {
		totalAmountToSpend uint64
		totalAmountToBurn  uint64
		appliedLockState   locked.State
		recipient          *secp256k1fx.OutputOwners
		change             *secp256k1fx.OutputOwners
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
			expectError: errInsufficientBalance,
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
			expectError: errInsufficientBalance,
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
			expectError: errInsufficientBalance,
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
			expectError: errInsufficientBalance,
			msg:         "Not enough balance to deposit",
		},
		"Self Transfer": {
			args: args{
				totalAmountToSpend: 1,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateUnlocked,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], []uint32{0}),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 4, outputOwners, ids.Empty, ids.Empty),
					},
				}
			},
		},
		"Self Transfer and change": {
			args: args{
				totalAmountToSpend: 1,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateUnlocked,
				change:             &changeOwners,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], []uint32{0}),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, outputOwners, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 3, changeOwners, ids.Empty, ids.Empty),
					},
				}
			},
		},
		"Recipient transfer and change": {
			args: args{
				totalAmountToSpend: 1,
				totalAmountToBurn:  1,
				appliedLockState:   locked.StateUnlocked,
				change:             &changeOwners,
				recipient:          &recipientOwners,
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{8, 8}, ctx.AVAXAssetID, 5, outputOwners, ids.Empty, ids.Empty),
			},
			generateWant: func(utxos []*avax.UTXO) want {
				return want{
					ins: []*avax.TransferableInput{
						generateTestInFromUTXO(utxos[0], []uint32{0}),
					},
					outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, recipientOwners, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 3, changeOwners, ids.Empty, ids.Empty),
					},
				}
			},
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

			testHandler := defaultCaminoHandler(t, internalState)

			ins, outs, signers, err := testHandler.Lock(
				[]*crypto.PrivateKeySECP256K1R{secpKey},
				tt.args.totalAmountToSpend,
				tt.args.totalAmountToBurn,
				tt.args.appliedLockState,
				tt.args.recipient,
				tt.args.change,
				0,
			)

			avax.SortTransferableOutputs(want.outs, txs.Codec)

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

	testHandler := defaultCaminoHandler(t, nil)

	assetID := testHandler.ctx.AVAXAssetID

	tx := &dummyUnsignedTx{txs.BaseTx{}}
	tx.Initialize([]byte{0})

	outputOwners1, cred1 := generateOwnersAndSig(tx)
	outputOwners2, cred2 := generateOwnersAndSig(tx)

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
			outs: []*avax.TransferableOutput{
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
			outs: []*avax.TransferableOutput{
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
		"utxos have stakable.LockedOut": {
			utxos: []*avax.UTXO{
				generateTestStakeableUTXO(ids.ID{1}, assetID, 10, 0, outputOwners1),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(assetID, 10, outputOwners1, ids.Empty, ids.Empty),
			},
			burnedAmount:     2,
			creds:            []verify.Verifiable{cred1},
			appliedLockState: locked.StateBonded,
			expectedErr:      errWrongUTXOOutType,
		},
		"outs have stakable.LockedOut": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 10, outputOwners1, ids.Empty, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestStakeableOut(assetID, 10, 0, outputOwners1),
			},
			burnedAmount:     2,
			creds:            []verify.Verifiable{cred1},
			appliedLockState: locked.StateBonded,
			expectedErr:      errWrongOutType,
		},
		"ins have stakable.LockedIn": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 10, outputOwners1, ids.Empty, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestStakeableIn(assetID, 10, 0, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(assetID, 10, outputOwners1, ids.Empty, ids.Empty),
			},
			burnedAmount:     2,
			creds:            []verify.Verifiable{cred1},
			appliedLockState: locked.StateBonded,
			expectedErr:      errWrongInType,
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

func TestGetDepositUnlockableAmounts(t *testing.T) {
	config := defaultConfig()
	ctx := snow.DefaultContextTest()
	baseDBManager := db_manager.NewMemDB(version.Semantic1_0_0)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	rewardsCalc := reward.NewCalculator(config.RewardConfig)
	addr0 := ids.GenerateTestShortID()
	addresses := set.NewSet[ids.ShortID](0)
	addresses.Add(addr0)

	depositTxSet := set.NewSet[ids.ID](0)
	testID := ids.GenerateTestID()
	depositTxSet.Add(testID)

	defaultState(config, ctx, baseDB, rewardsCalc)
	tx := &dummyUnsignedTx{txs.BaseTx{}}
	tx.Initialize([]byte{0})
	outputOwners, _ := generateOwnersAndSig(tx)
	now := time.Now()
	depositedAmount := uint64(1000)
	type args struct {
		state        func(*gomock.Controller) state.Chain
		depositTxIDs set.Set[ids.ID]
		currentTime  uint64
		addresses    set.Set[ids.ShortID]
	}
	tests := map[string]struct {
		args args
		want map[ids.ID]uint64
		err  error
	}{
		"Success retrieval of all unlockable amounts": {
			args: args{
				state: func(ctrl *gomock.Controller) state.Chain {
					nowMinus20m := uint64(now.Add(-20 * time.Minute).Unix())
					s := state.NewMockChain(ctrl)
					deposit1 := deposit.Deposit{
						DepositOfferID: testID,
						Start:          nowMinus20m,
						Duration:       uint32((20 * time.Minute).Seconds()),
						Amount:         depositedAmount,
					}
					s.EXPECT().GetDeposit(testID).Return(&deposit1, nil)
					s.EXPECT().GetDepositOffer(testID).Return(&deposit.Offer{
						Start:                nowMinus20m,
						UnlockPeriodDuration: uint32((20 * time.Minute).Seconds()),
					}, nil)
					return s
				},
				depositTxIDs: depositTxSet,
				currentTime:  uint64(now.Unix()),
				addresses:    outputOwners.AddressesSet(),
			},
			want: map[ids.ID]uint64{testID: depositedAmount},
		},
		"Success retrieval of 50% unlockable amounts": {
			args: args{
				state: func(ctrl *gomock.Controller) state.Chain {
					nowMinus20m := uint64(now.Add(-20 * time.Minute).Unix())
					s := state.NewMockChain(ctrl)
					deposit1 := deposit.Deposit{
						DepositOfferID: testID,
						Start:          nowMinus20m,
						Duration:       uint32((40 * time.Minute).Seconds()),
						Amount:         depositedAmount,
					}
					s.EXPECT().GetDeposit(testID).Return(&deposit1, nil)
					s.EXPECT().GetDepositOffer(testID).Return(&deposit.Offer{
						Start:                nowMinus20m,
						UnlockPeriodDuration: uint32((40 * time.Minute).Seconds()),
					}, nil)
					return s
				},
				depositTxIDs: depositTxSet,
				currentTime:  uint64(now.Unix()),
				addresses:    outputOwners.AddressesSet(),
			},
			want: map[ids.ID]uint64{testID: depositedAmount / 2},
		},
		"Failed to get deposit offer": {
			args: args{
				state: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetDeposit(testID).Return(&deposit.Deposit{DepositOfferID: testID}, nil)
					s.EXPECT().GetDepositOffer(testID).Return(nil, database.ErrNotFound)
					return s
				},
				depositTxIDs: depositTxSet,
				currentTime:  uint64(now.Unix()),
			},
			err: database.ErrNotFound,
		},
		"Failed to get deposit": {
			args: args{
				state: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetDeposit(gomock.Any()).Return(nil, errors.New("some_error"))
					return s
				},
				depositTxIDs: depositTxSet,
				currentTime:  uint64(now.Unix()),
			},
			err: fmt.Errorf("%w: %s", errFailToGetDeposit, "some_error"),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			got, err := getDepositUnlockableAmounts(test.args.state(ctrl), test.args.depositTxIDs, test.args.currentTime)

			if test.err != nil {
				require.ErrorContains(t, err, test.err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestUnlockDeposit(t *testing.T) {
	testHandler := defaultCaminoHandler(t, nil)
	ctx := testHandler.ctx

	testID := ids.GenerateTestID()
	txID := ids.GenerateTestID()
	depositedAmount := uint64(2000)
	outputOwners := defaultOwners()
	depositedUTXOs := []*avax.UTXO{
		generateTestUTXO(txID, ctx.AVAXAssetID, depositedAmount, outputOwners, testID, ids.Empty),
	}

	nowMinus10m := uint64(time.Now().Add(-10 * time.Minute).Unix())

	type args struct {
		state        func(*gomock.Controller) state.Chain
		keys         []*crypto.PrivateKeySECP256K1R
		depositTxIDs []ids.ID
	}
	sigIndices := []uint32{0}

	tests := map[string]struct {
		args  args
		want  []*avax.TransferableInput
		want1 []*avax.TransferableOutput
		want2 [][]*crypto.PrivateKeySECP256K1R
		err   error
	}{
		"Error retrieving unlockable amounts": {
			args: args{
				state: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					deposit1 := deposit.Deposit{
						DepositOfferID: testID,
						Start:          nowMinus10m,
						Duration:       uint32((10 * time.Minute).Seconds()),
					}
					depositTxSet := set.NewSet[ids.ID](1)
					depositTxSet.Add(testID)

					s.EXPECT().GetDeposit(testID).Return(&deposit1, nil)
					s.EXPECT().GetDepositOffer(testID).Return(&deposit.Offer{
						Start:                nowMinus10m,
						UnlockPeriodDuration: uint32((10 * time.Minute).Seconds()),
					}, nil)
					s.EXPECT().LockedUTXOs(depositTxSet, gomock.Any(), locked.StateDeposited).Return(nil, fmt.Errorf("%w: %s", state.ErrMissingParentState, testID))
					return s
				},
				keys:         preFundedKeys,
				depositTxIDs: []ids.ID{testID},
			},
			err: fmt.Errorf("%w: %s", state.ErrMissingParentState, testID),
		},
		"Successful unlock of 50% deposited funds": {
			args: args{
				state: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					deposit1 := deposit.Deposit{
						DepositOfferID: testID,
						Start:          nowMinus10m,
						Duration:       uint32((15 * time.Minute).Seconds()),
						Amount:         depositedAmount,
					}
					depositTxSet := set.NewSet[ids.ID](1)
					depositTxSet.Add(testID)

					s.EXPECT().GetDeposit(testID).Return(&deposit1, nil)
					s.EXPECT().GetDepositOffer(testID).Return(&deposit.Offer{
						Start:                nowMinus10m,
						UnlockPeriodDuration: uint32((10 * time.Minute).Seconds()),
					}, nil)
					s.EXPECT().LockedUTXOs(depositTxSet, gomock.Any(), locked.StateDeposited).Return(depositedUTXOs, nil)
					return s
				},
				keys:         []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
				depositTxIDs: []ids.ID{testID},
			},
			want: []*avax.TransferableInput{
				generateTestInFromUTXO(depositedUTXOs[0], sigIndices),
			},
			want1: []*avax.TransferableOutput{
				generateTestOut(ctx.AVAXAssetID, depositedAmount/2, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(ctx.AVAXAssetID, depositedAmount/2, outputOwners, testID, ids.Empty),
			},
			want2: [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}},
		},
		"Successful full unlock": {
			args: args{
				state: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					deposit1 := deposit.Deposit{
						DepositOfferID: testID,
						Start:          nowMinus10m,
						Duration:       uint32((10 * time.Minute).Seconds()),
						Amount:         depositedAmount,
					}
					depositTxSet := set.NewSet[ids.ID](1)
					depositTxSet.Add(testID)

					s.EXPECT().GetDeposit(testID).Return(&deposit1, nil)
					s.EXPECT().GetDepositOffer(testID).Return(&deposit.Offer{
						Start:                nowMinus10m,
						UnlockPeriodDuration: uint32((2 * time.Minute).Seconds()),
					}, nil)
					s.EXPECT().LockedUTXOs(depositTxSet, gomock.Any(), locked.StateDeposited).Return(depositedUTXOs, nil)
					return s
				},
				keys:         []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
				depositTxIDs: []ids.ID{testID},
			},
			want: []*avax.TransferableInput{
				generateTestInFromUTXO(depositedUTXOs[0], sigIndices),
			},
			want1: []*avax.TransferableOutput{
				generateTestOut(ctx.AVAXAssetID, depositedAmount, outputOwners, ids.Empty, ids.Empty),
			},
			want2: [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			got, got1, got2, err := testHandler.UnlockDeposit(tt.args.state(ctrl), tt.args.keys, tt.args.depositTxIDs)
			if tt.err != nil {
				require.ErrorContains(t, err, tt.err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got, "Error asserting TransferableInputs: got = %v, want %v", got, tt.want)
			require.Equal(t, tt.want1, got1, "Error asserting TransferableOutputs: got = %v, want %v", got1, tt.want2)
			require.Equal(t, tt.want2, got2, "UnlockDeposit() got = %v, want %v", got2, tt.want2)
		})
	}
}

func TestVerifyUnlockDepositedUTXOs(t *testing.T) {
	testHandler := defaultCaminoHandler(t, nil)
	ctx := testHandler.ctx

	tx := &dummyUnsignedTx{txs.BaseTx{}}
	tx.Initialize([]byte{0})
	var nilCreds *secp256k1fx.Credential
	outputOwners, cred1 := generateOwnersAndSig(tx)
	depositID := ids.GenerateTestID()
	otherID := ids.GenerateTestID()
	sigIndices := []uint32{0}

	deposit1 := &deposit.Deposit{
		DepositOfferID: ids.GenerateTestID(),
		UnlockedAmount: 0,
		Start:          0,
		Duration:       60,
		Amount:         1000,
	}
	depositOffer := &deposit.Offer{
		UnlockPeriodDuration: deposit1.Duration / 2,
	}
	depositNotExpiredTime := deposit1.EndTime().Add(-1 * time.Second)
	depositExpiredTime := deposit1.StartTime().Add(time.Duration(deposit1.Duration) * time.Second)
	unlockableAmount := deposit1.UnlockableAmount(depositOffer, uint64(depositNotExpiredTime.Unix()))

	type args struct {
		chainState   func(ctrl *gomock.Controller) state.Chain
		tx           txs.UnsignedTx
		utxos        []*avax.UTXO
		ins          []*avax.TransferableInput
		outs         []*avax.TransferableOutput
		creds        []verify.Verifiable
		burnedAmount uint64
		assetID      ids.ID
	}
	tests := map[string]struct {
		args args
		want map[ids.ID]uint64
		err  error
	}{
		"Inputs Credentials Mismatch": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					return s
				},
				utxos: []*avax.UTXO{{}},
				ins:   []*avax.TransferableInput{{}},
				creds: []verify.Verifiable{cred1, cred1},
			},
			err: errInputsCredentialsMismatch,
		},
		"Number of inputs-utxos mismatch": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					return state.NewMockChain(ctrl)
				},
				utxos: []*avax.UTXO{{}, {}},
				ins:   []*avax.TransferableInput{{}},
				creds: []verify.Verifiable{cred1},
			},
			err: fmt.Errorf("there are %d inputs and %d utxos: %w", 1, 2, errInputsUTXOSMismatch),
		},
		"Wrong credentials": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					return state.NewMockChain(ctrl)
				},
				utxos: []*avax.UTXO{{}},
				ins:   []*avax.TransferableInput{{}},
				creds: []verify.Verifiable{nilCreds},
			},
			err: errWrongCredentials,
		},
		"Lock IDs mismatch or no lockedOut output": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					return s
				},
				utxos: []*avax.UTXO{
					generateTestUTXO(depositID, ctx.AVAXAssetID, 1, outputOwners, ids.Empty, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, depositID, ids.Empty, sigIndices),
				},
				creds: []verify.Verifiable{cred1},
			},
			err: errLockIDsMismatch,
		},
		"UTXO AssetID mismatch": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					return s
				},
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 1, outputOwners, ids.Empty, depositID),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, sigIndices),
				},
				creds:   []verify.Verifiable{cred1},
				assetID: depositID,
			},
			err: fmt.Errorf("utxo %d has asset ID %s but expect %s: %w", 0, ctx.AVAXAssetID, depositID, errAssetIDMismatch),
		},
		"Input AssetID mismatch": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					return s
				},
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, depositID, 1, outputOwners, ids.Empty, depositID),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, sigIndices),
				},
				creds:   []verify.Verifiable{cred1},
				assetID: depositID,
			},
			err: errAssetIDMismatch,
		},
		"UTXO already unlocked": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					return s
				},
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 1, outputOwners, ids.Empty, depositID),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, sigIndices),
				},
				creds:   []verify.Verifiable{cred1},
				assetID: ctx.AVAXAssetID,
			},
			err: errUnlockingUnlockedUTXO,
		},
		"Locked funds not marked as locked": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					return s
				},
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 1, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, sigIndices),
				},
				creds:   []verify.Verifiable{cred1},
				assetID: ctx.AVAXAssetID,
			},
			err: errLockedFundsNotMarkedAsLocked,
		},
		"Consumed input amount mismatch": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 1, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1+1, depositID, ids.Empty, sigIndices),
				},
				creds:        []verify.Verifiable{cred1},
				burnedAmount: 0,
				assetID:      ctx.AVAXAssetID,
			},
			err: fmt.Errorf("failed to verify transfer: utxo inner out isn't *secp256k1fx.TransferOutput or inner out amount != input.Am"),
		},
		"Insufficient amount to cover burn fees (deposit not expired)": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositNotExpiredTime)
					s.EXPECT().GetDeposit(depositID).Return(deposit1, nil)
					s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, 1, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, depositID, ids.Empty, sigIndices),
				},
				creds:        []verify.Verifiable{cred1},
				burnedAmount: 1 + 1,
				assetID:      ctx.AVAXAssetID,
			},
			err: errNotBurnedEnough,
		},
		"Unlocked more deposited tokens than available (deposit not expired)": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositNotExpiredTime)
					s.EXPECT().GetDeposit(depositID).Return(deposit1, nil)
					s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, unlockableAmount+1, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, unlockableAmount+1, depositID, ids.Empty, sigIndices),
				},
				outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, unlockableAmount+1, outputOwners, ids.Empty, ids.Empty),
				},
				creds:        []verify.Verifiable{cred1},
				burnedAmount: 0,
				assetID:      ctx.AVAXAssetID,
			},
			err: fmt.Errorf("unlockedDepositAmount %d > %d unlockableAmount: %w", unlockableAmount+1, unlockableAmount, errUnlockedMoreThanAvailable),
		},
		"Produces outputs exceed inputs (deposit expired)": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, deposit1.Amount, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, deposit1.Amount, depositID, ids.Empty, sigIndices),
				},
				outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, deposit1.Amount+1, outputOwners, ids.Empty, ids.Empty),
				},
				creds:        []verify.Verifiable{&secp256k1fx.Credential{}},
				burnedAmount: 0,
				assetID:      ctx.AVAXAssetID,
			},
			err: errWrongProducedAmount,
		},
		"Consumed-produced amount mismatch": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositNotExpiredTime)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, unlockableAmount, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, unlockableAmount, depositID, ids.Empty, sigIndices),
				},
				outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, unlockableAmount, outputOwners, ids.Empty, otherID),
				},
				creds:        []verify.Verifiable{cred1},
				burnedAmount: 0,
				assetID:      ctx.AVAXAssetID,
			},
			err: errWrongProducedAmount,
		},
		"Partially consumed deposited amount (deposit expired)": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					s.EXPECT().GetDeposit(depositID).Return(deposit1, nil)
					s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, deposit1.Amount/2, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, deposit1.Amount/2, depositID, ids.Empty, sigIndices),
				},
				outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, deposit1.Amount/2, outputOwners, ids.Empty, ids.Empty),
				},
				creds:        []verify.Verifiable{cred1},
				burnedAmount: 0,
				assetID:      ctx.AVAXAssetID,
			},
			err: errNotConsumedDeposit,
		},
		"Success (expired deposit)": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositExpiredTime)
					s.EXPECT().GetDeposit(depositID).Return(deposit1, nil)
					s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, deposit1.Amount, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, deposit1.Amount, depositID, ids.Empty, sigIndices),
				},
				creds:        []verify.Verifiable{&secp256k1fx.Credential{}},
				burnedAmount: 0,
				assetID:      ctx.AVAXAssetID,
			},
			want: map[ids.ID]uint64{depositID: deposit1.Amount},
		},
		"Success (not expired deposit)": {
			args: args{
				chainState: func(ctrl *gomock.Controller) state.Chain {
					s := state.NewMockChain(ctrl)
					s.EXPECT().GetTimestamp().Return(depositNotExpiredTime)
					s.EXPECT().GetDeposit(depositID).Return(deposit1, nil)
					s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
					return s
				},
				tx: tx,
				utxos: []*avax.UTXO{
					generateTestUTXO(ids.ID{9, 9}, ctx.AVAXAssetID, unlockableAmount, outputOwners, depositID, ids.Empty),
				},
				ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, unlockableAmount, depositID, ids.Empty, sigIndices),
				},
				creds:        []verify.Verifiable{&secp256k1fx.Credential{}},
				burnedAmount: 0,
				assetID:      ctx.AVAXAssetID,
			},
			want: map[ids.ID]uint64{depositID: unlockableAmount},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			got, err := testHandler.VerifyUnlockDepositedUTXOs(tt.args.chainState(ctrl), tt.args.tx, tt.args.utxos, tt.args.ins, tt.args.outs, tt.args.creds, tt.args.burnedAmount, tt.args.assetID)

			if tt.err != nil {
				require.ErrorContains(t, err, tt.err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func defaultOwners() secp256k1fx.OutputOwners {
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
	}
	return outputOwners
}
