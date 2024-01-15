// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var _ txs.UnsignedTx = (*dummyUnsignedTx)(nil)

type dummyUnsignedTx struct {
	txs.BaseTx
}

func (*dummyUnsignedTx) Visit(txs.Visitor) error {
	return nil
}

func TestVerifyFinanceTx(t *testing.T) {
	fx := &secp256k1fx.Fx{}
	require.NoError(t, fx.InitializeVM(&secp256k1fx.TestVM{}))
	require.NoError(t, fx.Bootstrapped())

	ctx := snowtest.Context(t, snowtest.PChainID)
	keys := secp256k1.TestKeys()

	h := &handler{
		ctx: ctx,
		clk: &mockable.Clock{},
		fx:  fx,
	}

	cfg := &config.Config{
		FeeConfig: config.FeeConfig{
			DefaultUnitFees: commonfees.Dimensions{
				1 * units.MicroAvax,
				2 * units.MicroAvax,
				3 * units.MicroAvax,
				4 * units.MicroAvax,
			},
			DefaultBlockMaxConsumedUnits: commonfees.Dimensions{
				math.MaxUint64,
				math.MaxUint64,
				math.MaxUint64,
				math.MaxUint64,
			},
		},
	}

	var (
		bigUtxoTxID   = ids.GenerateTestID()
		bigUtxoKey    = keys[0]
		bigUtxoAddr   = bigUtxoKey.PublicKey().Address()
		bigUtxoAmount = 5 * units.MilliAvax
		bigUtxo       = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        bigUtxoTxID,
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: bigUtxoAmount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{bigUtxoAddr},
					Threshold: 1,
				},
			},
		}
		bigUtxoID = bigUtxo.InputID()

		smallUtxoTxID   = ids.GenerateTestID()
		smallUtxoKey    = keys[1]
		smallUtxoAddr   = smallUtxoKey.PublicKey().Address()
		smallUtxoAmount = 2 * units.MilliAvax
		smallUtxo       = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        smallUtxoTxID,
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: smallUtxoAmount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{smallUtxoAddr},
					Threshold: 1,
				},
			},
		}
		smallUtxoID = smallUtxo.InputID()
	)

	// this UTXOs ordering ensures that smallUtxo will be picked first,
	// even if bigUtxo would be enough finance the whole tx
	require.True(t, smallUtxoID.Compare(bigUtxoID) < 0)

	tests := []struct {
		description string
		utxoReaderF func(ctrl *gomock.Controller) avax.UTXOReader

		// keysF simplifies the utxoReade mock setup. We just specify here
		// the only keys referenced by the test scenario
		keysF         func() []*secp256k1.PrivateKey
		amountToStake uint64
		uTxF          func(t *testing.T) txs.UnsignedTx

		expectedErr error
		checksF     func(*testing.T, txs.UnsignedTx, *fees.Calculator, []*avax.TransferableInput, []*avax.TransferableOutput, []*avax.TransferableOutput)
	}{
		{
			description: "Tx, stake outputs,, multiple UTXOs",
			utxoReaderF: func(ctrl *gomock.Controller) avax.UTXOReader {
				s := state.NewMockState(ctrl)
				s.EXPECT().UTXOIDs(smallUtxoAddr.Bytes(), gomock.Any(), gomock.Any()).Return([]ids.ID{smallUtxoID}, nil).AnyTimes()
				s.EXPECT().GetUTXO(smallUtxoID).Return(smallUtxo, nil).AnyTimes()

				s.EXPECT().UTXOIDs(bigUtxoAddr.Bytes(), gomock.Any(), gomock.Any()).Return([]ids.ID{bigUtxoID}, nil).AnyTimes()
				s.EXPECT().GetUTXO(bigUtxoID).Return(bigUtxo, nil).AnyTimes()

				return s
			},
			keysF: func() []*secp256k1.PrivateKey {
				return []*secp256k1.PrivateKey{smallUtxoKey, bigUtxoKey}
			},

			amountToStake: units.MilliAvax,
			uTxF: func(t *testing.T) txs.UnsignedTx {
				uTx := &txs.AddValidatorTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							NetworkID:    ctx.NetworkID,
							BlockchainID: ctx.ChainID,
							Ins:          make([]*avax.TransferableInput, 0),
							Outs:         make([]*avax.TransferableOutput, 0),
							Memo:         []byte{'a', 'b', 'c'},
						},
					},
					Validator: txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  0,
						End:    uint64(time.Now().Unix()),
						Wght:   units.MilliAvax,
					},
					StakeOuts: make([]*avax.TransferableOutput, 0),
					RewardsOwner: &secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					DelegationShares: reward.PercentDenominator,
				}

				bytes, err := txs.Codec.Marshal(txs.CodecVersion, uTx)
				require.NoError(t, err)

				uTx.SetBytes(bytes)
				return uTx
			},
			expectedErr: nil,
			checksF: func(t *testing.T, uTx txs.UnsignedTx, calc *fees.Calculator, ins []*avax.TransferableInput, outs, staked []*avax.TransferableOutput) {
				r := require.New(t)
				expectedFee := 5879 * units.MicroAvax

				// complete uTx with the utxos
				addVal, ok := uTx.(*txs.AddValidatorTx)
				r.True(ok)

				addVal.Ins = ins
				addVal.Outs = outs
				addVal.StakeOuts = staked

				r.NoError(uTx.Visit(calc))
				r.Equal(expectedFee, calc.Fee)

				r.Len(ins, 2)
				r.Len(staked, 1)
				r.Len(outs, 1)
				r.Equal(expectedFee, ins[0].In.Amount()+ins[1].In.Amount()-staked[0].Out.Amount()-outs[0].Out.Amount())
			},
		},
		{
			description: "Tx, stake outputs, single UTXO",
			utxoReaderF: func(ctrl *gomock.Controller) avax.UTXOReader {
				s := state.NewMockState(ctrl)
				s.EXPECT().UTXOIDs(bigUtxoAddr.Bytes(), gomock.Any(), gomock.Any()).Return([]ids.ID{bigUtxoID}, nil).AnyTimes()
				s.EXPECT().GetUTXO(bigUtxoID).Return(bigUtxo, nil).AnyTimes()
				return s
			},
			keysF: func() []*secp256k1.PrivateKey {
				return []*secp256k1.PrivateKey{bigUtxoKey}
			},

			amountToStake: units.MilliAvax,
			uTxF: func(t *testing.T) txs.UnsignedTx {
				uTx := &txs.AddValidatorTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							NetworkID:    ctx.NetworkID,
							BlockchainID: ctx.ChainID,
							Ins:          make([]*avax.TransferableInput, 0),
							Outs:         make([]*avax.TransferableOutput, 0),
							Memo:         []byte{'a', 'b', 'c'},
						},
					},
					Validator: txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  0,
						End:    uint64(time.Now().Unix()),
						Wght:   units.MilliAvax,
					},
					StakeOuts: make([]*avax.TransferableOutput, 0),
					RewardsOwner: &secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					DelegationShares: reward.PercentDenominator,
				}

				bytes, err := txs.Codec.Marshal(txs.CodecVersion, uTx)
				require.NoError(t, err)

				uTx.SetBytes(bytes)
				return uTx
			},
			expectedErr: nil,
			checksF: func(t *testing.T, uTx txs.UnsignedTx, calc *fees.Calculator, ins []*avax.TransferableInput, outs, staked []*avax.TransferableOutput) {
				r := require.New(t)
				expectedFee := 3341 * units.MicroAvax

				// complete uTx with the utxos
				addVal, ok := uTx.(*txs.AddValidatorTx)
				r.True(ok)

				addVal.Ins = ins
				addVal.Outs = outs
				addVal.StakeOuts = staked

				r.NoError(uTx.Visit(calc))
				r.Equal(expectedFee, calc.Fee)

				r.Len(ins, 1)
				r.Len(staked, 1)
				r.Len(outs, 1)
				r.Equal(expectedFee, ins[0].In.Amount()-staked[0].Out.Amount()-outs[0].Out.Amount())
			},
		},
		{
			description: "Tx, no stake outputs, single UTXO",
			utxoReaderF: func(ctrl *gomock.Controller) avax.UTXOReader {
				s := state.NewMockState(ctrl)
				s.EXPECT().UTXOIDs(bigUtxoAddr.Bytes(), gomock.Any(), gomock.Any()).Return([]ids.ID{bigUtxoID}, nil).AnyTimes()
				s.EXPECT().GetUTXO(bigUtxoID).Return(bigUtxo, nil).AnyTimes()
				return s
			},
			keysF: func() []*secp256k1.PrivateKey {
				return []*secp256k1.PrivateKey{bigUtxoKey}
			},

			amountToStake: 0,
			uTxF: func(t *testing.T) txs.UnsignedTx {
				uTx := &txs.CreateChainTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							NetworkID:    ctx.NetworkID,
							BlockchainID: ctx.ChainID,
							Ins:          make([]*avax.TransferableInput, 0),
							Outs:         make([]*avax.TransferableOutput, 0),
							Memo:         []byte{'a', 'b', 'c'},
						},
					},
					SubnetID:   ids.GenerateTestID(),
					ChainName:  "testChain",
					VMID:       ids.GenerateTestID(),
					SubnetAuth: &secp256k1fx.Input{},
				}

				bytes, err := txs.Codec.Marshal(txs.CodecVersion, uTx)
				require.NoError(t, err)

				uTx.SetBytes(bytes)
				return uTx
			},
			expectedErr: nil,
			checksF: func(t *testing.T, uTx txs.UnsignedTx, calc *fees.Calculator, ins []*avax.TransferableInput, outs, staked []*avax.TransferableOutput) {
				r := require.New(t)
				expectedFee := 3014 * units.MicroAvax

				// complete uTx with the utxos
				addVal, ok := uTx.(*txs.CreateChainTx)
				r.True(ok)

				addVal.Ins = ins
				addVal.Outs = outs

				r.NoError(uTx.Visit(calc))
				r.Equal(expectedFee, calc.Fee)

				r.Len(ins, 1)
				r.Len(outs, 1)
				r.Equal(expectedFee, ins[0].In.Amount()-outs[0].Out.Amount())
				r.Empty(staked)
			},
		},
		{
			description: "Tx, no stake outputs, multiple UTXOs",
			utxoReaderF: func(ctrl *gomock.Controller) avax.UTXOReader {
				s := state.NewMockState(ctrl)
				s.EXPECT().UTXOIDs(smallUtxoAddr.Bytes(), gomock.Any(), gomock.Any()).Return([]ids.ID{smallUtxoID}, nil).AnyTimes()
				s.EXPECT().GetUTXO(smallUtxoID).Return(smallUtxo, nil).AnyTimes()

				s.EXPECT().UTXOIDs(bigUtxoAddr.Bytes(), gomock.Any(), gomock.Any()).Return([]ids.ID{bigUtxoID}, nil).AnyTimes()
				s.EXPECT().GetUTXO(bigUtxoID).Return(bigUtxo, nil).AnyTimes()

				return s
			},
			keysF: func() []*secp256k1.PrivateKey {
				return []*secp256k1.PrivateKey{smallUtxoKey, bigUtxoKey}
			},

			amountToStake: 0,
			uTxF: func(t *testing.T) txs.UnsignedTx {
				uTx := &txs.CreateChainTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							NetworkID:    ctx.NetworkID,
							BlockchainID: ctx.ChainID,
							Ins:          make([]*avax.TransferableInput, 0),
							Outs:         make([]*avax.TransferableOutput, 0),
							Memo:         []byte{'a', 'b', 'c'},
						},
					},
					SubnetID:   ids.GenerateTestID(),
					ChainName:  "testChain",
					VMID:       ids.GenerateTestID(),
					SubnetAuth: &secp256k1fx.Input{},
				}

				bytes, err := txs.Codec.Marshal(txs.CodecVersion, uTx)
				require.NoError(t, err)

				uTx.SetBytes(bytes)
				return uTx
			},
			expectedErr: nil,
			checksF: func(t *testing.T, uTx txs.UnsignedTx, calc *fees.Calculator, ins []*avax.TransferableInput, outs, staked []*avax.TransferableOutput) {
				r := require.New(t)
				expectedFee := 5552 * units.MicroAvax

				// complete uTx with the utxos
				addVal, ok := uTx.(*txs.CreateChainTx)
				r.True(ok)

				addVal.Ins = ins
				addVal.Outs = outs

				r.NoError(uTx.Visit(calc))
				r.Equal(expectedFee, calc.Fee)

				r.Len(ins, 2)
				r.Len(outs, 1)
				r.Equal(expectedFee, ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
				r.Empty(staked)
			},
		},
		{
			description: "no inputs, no outputs, no fee",
			utxoReaderF: func(ctrl *gomock.Controller) avax.UTXOReader {
				s := state.NewMockState(ctrl)
				s.EXPECT().UTXOIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return([]ids.ID{}, nil).AnyTimes()
				return s
			},
			keysF: func() []*secp256k1.PrivateKey {
				return []*secp256k1.PrivateKey{}
			},
			amountToStake: 0,
			uTxF: func(t *testing.T) txs.UnsignedTx {
				unsignedTx := dummyUnsignedTx{
					BaseTx: txs.BaseTx{},
				}
				unsignedTx.SetBytes([]byte{0})
				return &unsignedTx
			},
			expectedErr: nil,
			checksF: func(t *testing.T, uTx txs.UnsignedTx, calc *fees.Calculator, ins []*avax.TransferableInput, outs, staked []*avax.TransferableOutput) {
				r := require.New(t)

				r.NoError(uTx.Visit(calc))
				r.Zero(calc.Fee)

				r.Empty(ins)
				r.Empty(outs)
				r.Empty(staked)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			r := require.New(t)
			ctrl := gomock.NewController(t)

			uTx := test.uTxF(t)

			fm := commonfees.NewManager(cfg.DefaultUnitFees)
			feeCalc := &fees.Calculator{
				FeeManager:  fm,
				Config:      cfg,
				ChainTime:   time.Time{},
				Credentials: []verify.Verifiable{},
			}

			// init fee calc with the uTx data
			r.NoError(uTx.Visit(feeCalc))

			ins, outs, staked, _, err := h.FinanceTx(
				test.utxoReaderF(ctrl),
				test.keysF(),
				test.amountToStake,
				feeCalc,
				ids.GenerateTestShortID(),
			)
			r.ErrorIs(err, test.expectedErr)
			test.checksF(t, uTx, feeCalc, ins, outs, staked)
		})
	}
}

func TestVerifySpendUTXOs(t *testing.T) {
	fx := &secp256k1fx.Fx{}

	require.NoError(t, fx.InitializeVM(&secp256k1fx.TestVM{}))
	require.NoError(t, fx.Bootstrapped())

	ctx := snowtest.Context(t, snowtest.PChainID)

	h := &handler{
		ctx: ctx,
		clk: &mockable.Clock{},
		fx:  fx,
	}

	// The handler time during a test, unless [chainTimestamp] is set
	now := time.Unix(1607133207, 0)

	unsignedTx := dummyUnsignedTx{
		BaseTx: txs.BaseTx{},
	}
	unsignedTx.SetBytes([]byte{0})

	customAssetID := ids.GenerateTestID()

	// Note that setting [chainTimestamp] also set's the handler's clock.
	// Adjust input/output locktimes accordingly.
	tests := []struct {
		description     string
		utxos           []*avax.UTXO
		ins             []*avax.TransferableInput
		outs            []*avax.TransferableOutput
		creds           []verify.Verifiable
		producedAmounts map[ids.ID]uint64
		expectedErr     error
	}{
		{
			description:     "no inputs, no outputs, no fee",
			utxos:           []*avax.UTXO{},
			ins:             []*avax.TransferableInput{},
			outs:            []*avax.TransferableOutput{},
			creds:           []verify.Verifiable{},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     nil,
		},
		{
			description: "no inputs, no outputs, positive fee",
			utxos:       []*avax.UTXO{},
			ins:         []*avax.TransferableInput{},
			outs:        []*avax.TransferableOutput{},
			creds:       []verify.Verifiable{},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: ErrInsufficientUnlockedFunds,
		},
		{
			description: "wrong utxo assetID, one input, no outputs, no fee",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: customAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     errAssetIDMismatch,
		},
		{
			description: "one wrong assetID input, no outputs, no fee",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: customAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     errAssetIDMismatch,
		},
		{
			description: "one input, one wrong assetID output, no fee",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     ErrInsufficientUnlockedFunds,
		},
		{
			description: "attempt to consume locked output as unlocked",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Add(time.Second).Unix()),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     errLockedFundsNotMarkedAsLocked,
		},
		{
			description: "attempt to modify locktime",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Add(time.Second).Unix()),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &stakeable.LockIn{
					Locktime: uint64(now.Unix()),
					TransferableIn: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     errLocktimeMismatch,
		},
		{
			description: "one input, no outputs, positive fee",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: nil,
		},
		{
			description: "wrong number of credentials",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs:  []*avax.TransferableOutput{},
			creds: []verify.Verifiable{},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: errWrongNumberCredentials,
		},
		{
			description: "wrong number of UTXOs",
			utxos:       []*avax.UTXO{},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: errWrongNumberUTXOs,
		},
		{
			description: "invalid credential",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				(*secp256k1fx.Credential)(nil),
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: secp256k1fx.ErrNilCredential,
		},
		{
			description: "invalid signature",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: [][secp256k1.SignatureLen]byte{
						{},
					},
				},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: secp256k1.ErrInvalidSig,
		},
		{
			description: "one input, no outputs, positive fee",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: nil,
		},
		{
			description: "locked one input, no outputs, no fee",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Unix()) + 1,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &stakeable.LockIn{
					Locktime: uint64(now.Unix()) + 1,
					TransferableIn: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     nil,
		},
		{
			description: "locked one input, no outputs, positive fee",
			utxos: []*avax.UTXO{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Unix()) + 1,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*avax.TransferableInput{{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In: &stakeable.LockIn{
					Locktime: uint64(now.Unix()) + 1,
					TransferableIn: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			}},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: ErrInsufficientUnlockedFunds,
		},
		{
			description: "one locked and one unlocked input, one locked output, positive fee",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &stakeable.LockIn{
						Locktime: uint64(now.Unix()) + 1,
						TransferableIn: &secp256k1fx.TransferInput{
							Amt: 1,
						},
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: nil,
		},
		{
			description: "one locked and one unlocked input, one locked output, positive fee, partially locked",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 2,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &stakeable.LockIn{
						Locktime: uint64(now.Unix()) + 1,
						TransferableIn: &secp256k1fx.TransferInput{
							Amt: 1,
						},
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 2,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: nil,
		},
		{
			description: "one unlocked input, one locked output, zero fee",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) - 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     nil,
		},
		{
			description: "attempted overflow",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 2,
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     safemath.ErrOverflow,
		},
		{
			description: "attempted mint",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     ErrInsufficientLockedFunds,
		},
		{
			description: "attempted mint through locking",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: math.MaxUint64,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     safemath.ErrOverflow,
		},
		{
			description: "attempted mint through mixed locking (low then high)",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 2,
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: math.MaxUint64,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     ErrInsufficientLockedFunds,
		},
		{
			description: "attempted mint through mixed locking (high then low)",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64,
					},
				},
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     ErrInsufficientLockedFunds,
		},
		{
			description: "transfer non-avax asset",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     nil,
		},
		{
			description: "lock non-avax asset",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Add(time.Second).Unix()),
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     nil,
		},
		{
			description: "attempted asset conversion",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			expectedErr:     ErrInsufficientUnlockedFunds,
		},
		{
			description: "attempted asset conversion with burn",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: ErrInsufficientUnlockedFunds,
		},
		{
			description: "two inputs, one output with custom asset, with fee",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: nil,
		},
		{
			description: "one input, fee, custom asset",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
			},
			expectedErr: ErrInsufficientUnlockedFunds,
		},
		{
			description: "one input, custom fee",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				customAssetID: 1,
			},
			expectedErr: nil,
		},
		{
			description: "one input, custom fee, wrong burn",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				customAssetID: 1,
			},
			expectedErr: ErrInsufficientUnlockedFunds,
		},
		{
			description: "two inputs, multiple fee",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.AVAXAssetID: 1,
				customAssetID:     1,
			},
			expectedErr: nil,
		},
		{
			description: "one unlock input, one locked output, zero fee, unlocked, custom asset",
			utxos: []*avax.UTXO{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) - 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			ins: []*avax.TransferableInput{
				{
					Asset: avax.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: make(map[ids.ID]uint64),
			expectedErr:     nil,
		},
	}

	for _, test := range tests {
		h.clk.Set(now)

		t.Run(test.description, func(t *testing.T) {
			err := h.VerifySpendUTXOs(
				&unsignedTx,
				test.utxos,
				test.ins,
				test.outs,
				test.creds,
				test.producedAmounts,
			)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
