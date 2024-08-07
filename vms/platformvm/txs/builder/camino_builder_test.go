// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	testState "github.com/ava-labs/avalanchego/vms/platformvm/state/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/expect"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	deposits "github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

// only tests pre-berlin upgrade version 0
func TestNewAddressStateTx(t *testing.T) {
	ctx := test.Context(t)

	fundsKey := test.FundedKeys[0]
	fundsAddr := fundsKey.Address()
	fundsOwner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{fundsAddr}}

	otherAddr := ids.ShortID{1, 1}

	feeUTXO := generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.TxFee, fundsOwner, ids.Empty, ids.Empty, false)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: []*avax.TransferableInput{
			generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
		},
		Outs: []*avax.TransferableOutput{},
	}}

	tests := map[string]struct {
		state       func(*gomock.Controller) state.State
		targetAddr  ids.ShortID
		remove      bool
		stateBit    as.AddressStateBit
		executor    ids.ShortID
		keys        []*secp256k1.PrivateKey
		change      *secp256k1fx.OutputOwners
		utx         *txs.AddressStateTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Empty address": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{fundsAddr: {feeUTXO}})
				s.EXPECT().GetTimestamp().Return(test.LatestPhaseTime)
				return s
			},
			stateBit:    as.AddressStateBitKYCVerified,
			keys:        []*secp256k1.PrivateKey{fundsKey},
			signers:     [][]*secp256k1.PrivateKey{{fundsKey}},
			expectedErr: errEmptyAddress,
		},
		"OK": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{fundsAddr: {feeUTXO}})
				s.EXPECT().GetTimestamp().Return(test.LatestPhaseTime)
				return s
			},
			targetAddr: otherAddr,
			stateBit:   as.AddressStateBitKYCVerified,
			keys:       []*secp256k1.PrivateKey{fundsKey},
			utx: &txs.AddressStateTx{
				BaseTx:   baseTx,
				Address:  otherAddr,
				StateBit: as.AddressStateBitKYCVerified,
			},
			signers: [][]*secp256k1.PrivateKey{{fundsKey}},
		},
		"OK: remove": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{fundsAddr: {feeUTXO}})
				s.EXPECT().GetTimestamp().Return(test.LatestPhaseTime)
				return s
			},
			targetAddr: otherAddr,
			remove:     true,
			stateBit:   as.AddressStateBitKYCVerified,
			keys:       []*secp256k1.PrivateKey{fundsKey},
			utx: &txs.AddressStateTx{
				BaseTx:   baseTx,
				Address:  otherAddr,
				StateBit: as.AddressStateBitKYCVerified,
				Remove:   true,
			},
			signers: [][]*secp256k1.PrivateKey{{fundsKey}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			// only tests pre-berlin upgrade version 0
			b := newCaminoBuilder(t, tt.state(ctrl), nil, test.PhaseAthens)

			tx, err := b.NewAddressStateTx(
				tt.targetAddr,
				tt.remove,
				tt.stateBit,
				tt.executor,
				tt.keys,
				tt.change,
			)
			require.ErrorIs(err, tt.expectedErr)
			if err != nil {
				require.Nil(tx)
				return
			}
			expectedTx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)
			require.NoError(expectedTx.SyntacticVerify(b.ctx))
			require.Equal(expectedTx, tx)
		})
	}
}

func TestNewAddSubnetValidatorTx(t *testing.T) {
	ctx := test.Context(t)

	subnetOwnerKey := test.FundedKeys[1]
	subnetOwnerAddr := subnetOwnerKey.Address()
	fundsKey := test.FundedKeys[0]
	fundsAddr := fundsKey.Address()
	nodeKey1 := test.FundedNodeKeys[0]
	nodeID1 := test.FundedNodeIDs[0]
	nodeKey2 := test.FundedNodeKeys[1]
	nodeID2 := test.FundedNodeIDs[1]

	fundsOwner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{fundsAddr}}
	subnetOwner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{subnetOwnerAddr}}
	subnetTx := &txs.Tx{Unsigned: &txs.CreateSubnetTx{Owner: &subnetOwner}}
	subnetID := ids.ID{2}

	feeUTXO := generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.TxFee, fundsOwner, ids.Empty, ids.Empty, false)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: []*avax.TransferableInput{
			generate.InFromUTXO(t, feeUTXO, []uint32{0}, true),
		},
		Outs: []*avax.TransferableOutput{},
	}}

	tests := map[string]struct {
		state          func(*gomock.Controller, api.Camino) state.State
		caminoConfig   api.Camino
		weight         uint64
		startTimestamp uint64
		endTimestamp   uint64
		nodeID         ids.NodeID
		subnetID       ids.ID
		keys           []*secp256k1.PrivateKey
		change         ids.ShortID
		utx            *txs.AddSubnetValidatorTx
		signers        [][]*secp256k1.PrivateKey
		expectedErr    error
	}{
		"NodeID key mismatch: LockModeBondDeposit false": {
			state: func(ctrl *gomock.Controller, cfg api.Camino) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().GetTx(subnetID).Return(subnetTx, status.Committed, nil)
				s.EXPECT().CaminoConfig().Return(testState.StateConfigFromAPIConfig(cfg), nil)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{
					fundsAddr:            {feeUTXO},
					ids.ShortID(nodeID2): {},
					subnetOwnerAddr:      {},
				})
				return s
			},
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			weight:   test.ValidatorWeight,
			nodeID:   nodeID1,
			subnetID: subnetID,
			keys:     []*secp256k1.PrivateKey{fundsKey, subnetOwnerKey, nodeKey2},
			utx: &txs.AddSubnetValidatorTx{
				BaseTx: baseTx,
				SubnetValidator: txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID1,
						Wght:   test.ValidatorWeight,
					},
					Subnet: subnetID,
				},
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{fundsKey}, {subnetOwnerKey}, {nodeKey2},
			},
			expectedErr: errKeyMissing,
		},
		"NodeID key mismatch: LockModeBondDeposit true": {
			state: func(ctrl *gomock.Controller, cfg api.Camino) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().GetTx(subnetID).Return(subnetTx, status.Committed, nil)
				s.EXPECT().CaminoConfig().Return(testState.StateConfigFromAPIConfig(cfg), nil)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{
					fundsAddr:            {feeUTXO},
					ids.ShortID(nodeID2): {},
					subnetOwnerAddr:      {},
				})
				return s
			},
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			weight:   test.ValidatorWeight,
			nodeID:   nodeID1,
			subnetID: subnetID,
			keys:     []*secp256k1.PrivateKey{fundsKey, subnetOwnerKey, nodeKey2},
			utx: &txs.AddSubnetValidatorTx{
				BaseTx: baseTx,
				SubnetValidator: txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID1,
						Wght:   test.ValidatorWeight,
					},
					Subnet: subnetID,
				},
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{fundsKey}, {subnetOwnerKey}, {nodeKey2},
			},
			expectedErr: errKeyMissing,
		},
		"OK: LockModeBondDeposit false": {
			state: func(ctrl *gomock.Controller, cfg api.Camino) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().GetTx(subnetID).Return(subnetTx, status.Committed, nil)
				s.EXPECT().CaminoConfig().Return(testState.StateConfigFromAPIConfig(cfg), nil)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{
					fundsAddr:            {feeUTXO},
					ids.ShortID(nodeID1): {},
					subnetOwnerAddr:      {},
				})
				return s
			},
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			weight:   test.ValidatorWeight,
			nodeID:   nodeID1,
			subnetID: subnetID,
			keys:     []*secp256k1.PrivateKey{fundsKey, subnetOwnerKey, nodeKey1},
			utx: &txs.AddSubnetValidatorTx{
				BaseTx: baseTx,
				SubnetValidator: txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID1,
						Wght:   test.ValidatorWeight,
					},
					Subnet: subnetID,
				},
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{fundsKey}, {subnetOwnerKey}, {nodeKey1},
			},
		},
		"OK: LockModeBondDeposit true": {
			state: func(ctrl *gomock.Controller, cfg api.Camino) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().GetTx(subnetID).Return(subnetTx, status.Committed, nil)
				s.EXPECT().CaminoConfig().Return(testState.StateConfigFromAPIConfig(cfg), nil)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{
					fundsAddr:            {feeUTXO},
					ids.ShortID(nodeID1): {},
					subnetOwnerAddr:      {},
				})
				return s
			},
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			weight:   test.ValidatorWeight,
			nodeID:   nodeID1,
			subnetID: subnetID,
			keys:     []*secp256k1.PrivateKey{fundsKey, subnetOwnerKey, nodeKey1},
			utx: &txs.AddSubnetValidatorTx{
				BaseTx: baseTx,
				SubnetValidator: txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID1,
						Wght:   test.ValidatorWeight,
					},
					Subnet: subnetID,
				},
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{fundsKey}, {subnetOwnerKey}, {nodeKey1},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			b := newCaminoBuilder(t, tt.state(ctrl, tt.caminoConfig), nil, test.PhaseLast)

			tx, err := b.NewAddSubnetValidatorTx(
				tt.weight,
				tt.startTimestamp,
				tt.endTimestamp,
				tt.nodeID,
				tt.subnetID,
				tt.keys,
				tt.change,
			)
			require.ErrorIs(err, tt.expectedErr)
			if err != nil {
				require.Nil(tx)
				return
			}
			expectedTx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)
			require.NoError(expectedTx.SyntacticVerify(b.ctx))
			require.Equal(expectedTx, tx)
		})
	}
}

func TestNewUnlockDepositTx(t *testing.T) {
	ctx := test.Context(t)

	fundsKey := test.FundedKeys[0]
	fundsAddr := fundsKey.Address()
	fundsOwner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{fundsAddr}}

	depositTxID := ids.ID{1, 1}
	bondTxID := ids.ID{2, 2}
	offerID := ids.ID{3, 3}

	depositedAmount := uint64(100)
	unlockedAmount := depositedAmount / 2

	unlockedUTXO := generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.TxFee, fundsOwner, ids.Empty, ids.Empty, false)
	depositedUTXO := generate.UTXO(ids.ID{2}, ctx.AVAXAssetID, depositedAmount/4, fundsOwner, depositTxID, ids.Empty, false)
	depositedBondedUTXO := generate.UTXO(ids.ID{3}, ctx.AVAXAssetID, depositedAmount/4, fundsOwner, depositTxID, bondTxID, false)

	tests := map[string]struct {
		state        func(*gomock.Controller) state.State
		depositTxIDs []ids.ID
		keys         []*secp256k1.PrivateKey
		change       *secp256k1fx.OutputOwners
		utx          *txs.UnlockDepositTx
		signers      [][]*secp256k1.PrivateKey
		expectedErr  error
	}{
		"OK": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expect.UnlockDeposit(t, s,
					map[ids.ID]*deposits.Deposit{
						depositTxID: {
							DepositOfferID: offerID,
							Start:          test.GenesisTimestamp,
							UnlockedAmount: unlockedAmount,
							Amount:         depositedAmount,
						},
					},
					[]*deposits.Offer{{ID: offerID}},
					[]ids.ShortID{fundsAddr},
					[]*avax.UTXO{depositedUTXO, depositedBondedUTXO},
					locked.StateDeposited,
				)
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{fundsAddr: {unlockedUTXO, depositedUTXO, depositedBondedUTXO}})
				return s
			},
			depositTxIDs: []ids.ID{depositTxID},
			keys:         []*secp256k1.PrivateKey{fundsKey},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Ins: []*avax.TransferableInput{
					generate.InFromUTXO(t, unlockedUTXO, []uint32{0}, false),
					generate.InFromUTXO(t, depositedUTXO, []uint32{0}, false),
					generate.InFromUTXO(t, depositedBondedUTXO, []uint32{0}, false),
				},
				Outs: []*avax.TransferableOutput{
					generate.OutFromUTXO(t, depositedUTXO, ids.Empty, ids.Empty),
					generate.OutFromUTXO(t, depositedBondedUTXO, ids.Empty, bondTxID),
				},
			}}},
			signers: [][]*secp256k1.PrivateKey{
				{fundsKey}, {fundsKey}, {fundsKey},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			b := newCaminoBuilder(t, tt.state(ctrl), nil, test.PhaseLast)

			tx, err := b.NewUnlockDepositTx(tt.depositTxIDs, tt.keys, tt.change)
			require.ErrorIs(err, tt.expectedErr)
			if err != nil {
				require.Nil(tx)
				return
			}
			expectedTx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)
			require.NoError(expectedTx.SyntacticVerify(b.ctx))
			require.Equal(expectedTx, tx)
		})
	}
}

func TestNewClaimTx(t *testing.T) {
	ctx := test.Context(t)

	caminoConfig := &state.CaminoConfig{
		LockModeBondDeposit: true,
	}

	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()

	feeKey, feeAddr, feeUTXOOwner := generate.KeyAndOwner(t, test.Keys[0])
	rewardOwner1Key, rewardOwner1Addr, rewardOwner1 := generate.KeyAndOwner(t, test.Keys[1])
	rewardOwner2Key, rewardOwner2Addr, rewardOwner2 := generate.KeyAndOwner(t, test.Keys[2])
	claimableOwnerID := ids.GenerateTestID()

	feeUTXO := generate.UTXO(ids.GenerateTestID(), ctx.AVAXAssetID, test.TxFee, feeUTXOOwner, ids.Empty, ids.Empty, true)

	baseTxWithFeeInput := func(outs []*avax.TransferableOutput) *txs.BaseTx {
		return &txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        feeUTXO.TxID,
						OutputIndex: feeUTXO.OutputIndex,
					},
					Asset: feeUTXO.Asset,
					In: &secp256k1fx.TransferInput{
						Amt:   test.TxFee,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: outs,
			},
			SyntacticallyVerified: true,
		}
	}

	type args struct {
		claimables []txs.ClaimAmount
		claimTo    *secp256k1fx.OutputOwners
		keys       []*secp256k1.PrivateKey
		change     *secp256k1fx.OutputOwners
	}

	tests := map[string]struct {
		state       func(*gomock.Controller) state.State
		args        args
		expectedTx  func(t *testing.T) *txs.Tx
		expectedErr error
	}{
		"OK, single deposit tx": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}})
				// deposits
				s.EXPECT().GetDeposit(depositTxID1).Return(&deposits.Deposit{RewardOwner: &rewardOwner1}, nil)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{{
					ID: depositTxID1, Type: txs.ClaimTypeActiveDepositReward, Amount: 11,
				}},
				claimTo: &rewardOwner1,
				keys: []*secp256k1.PrivateKey{
					feeKey,
					rewardOwner1Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          11,
							OutputOwners: rewardOwner1,
						},
					}}),
					Claimables: []txs.ClaimAmount{{
						ID:        depositTxID1,
						Type:      txs.ClaimTypeActiveDepositReward,
						Amount:    11,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					}},
				}, txs.Codec, [][]*secp256k1.PrivateKey{
					{feeKey},
					{rewardOwner1Key},
				})
				require.NoError(t, err)
				return tx
			},
			expectedErr: nil,
		},
		"OK, two deposit tx": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}, rewardOwner2Addr: {}})
				// deposits
				s.EXPECT().GetDeposit(depositTxID1).Return(&deposits.Deposit{RewardOwner: &rewardOwner1}, nil)
				s.EXPECT().GetDeposit(depositTxID2).Return(&deposits.Deposit{RewardOwner: &rewardOwner2}, nil)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{
					{ID: depositTxID1, Type: txs.ClaimTypeActiveDepositReward, Amount: 11},
					{ID: depositTxID2, Type: txs.ClaimTypeActiveDepositReward, Amount: 22},
				},
				claimTo: &rewardOwner1,
				keys: []*secp256k1.PrivateKey{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{
						{
							Asset: avax.Asset{ID: ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt:          11,
								OutputOwners: rewardOwner1,
							},
						},
						{
							Asset: avax.Asset{ID: ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt:          22,
								OutputOwners: rewardOwner1,
							},
						},
					}),
					Claimables: []txs.ClaimAmount{
						{
							ID:        depositTxID1,
							Type:      txs.ClaimTypeActiveDepositReward,
							Amount:    11,
							OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
						},
						{
							ID:        depositTxID2,
							Type:      txs.ClaimTypeActiveDepositReward,
							Amount:    22,
							OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				}, txs.Codec, [][]*secp256k1.PrivateKey{
					{feeKey},
					{rewardOwner1Key},
					{rewardOwner2Key},
				})
				require.NoError(t, err)
				return tx
			},
			expectedErr: nil,
		},
		"OK, two deposit tx, owner keys intersect": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}, rewardOwner2Addr: {}})
				// deposits
				s.EXPECT().GetDeposit(depositTxID1).Return(&deposits.Deposit{
					RewardOwner: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs:     []ids.ShortID{rewardOwner1Addr, rewardOwner2Addr},
					},
				}, nil)
				s.EXPECT().GetDeposit(depositTxID2).Return(&deposits.Deposit{RewardOwner: &rewardOwner1}, nil)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{
					{ID: depositTxID1, Type: txs.ClaimTypeActiveDepositReward, Amount: 11},
					{ID: depositTxID2, Type: txs.ClaimTypeActiveDepositReward, Amount: 22},
				},
				claimTo: &rewardOwner1,
				keys: []*secp256k1.PrivateKey{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{
						{
							Asset: avax.Asset{ID: ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt:          11,
								OutputOwners: rewardOwner1,
							},
						},
						{
							Asset: avax.Asset{ID: ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt:          22,
								OutputOwners: rewardOwner1,
							},
						},
					}),
					Claimables: []txs.ClaimAmount{
						{
							ID:        depositTxID1,
							Type:      txs.ClaimTypeActiveDepositReward,
							Amount:    11,
							OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
						},
						{
							ID:        depositTxID2,
							Type:      txs.ClaimTypeActiveDepositReward,
							Amount:    22,
							OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				}, txs.Codec, [][]*secp256k1.PrivateKey{
					{feeKey},
					{rewardOwner1Key, rewardOwner2Key},
					{rewardOwner1Key},
				})
				require.NoError(t, err)
				return tx
			},
			expectedErr: nil,
		},
		"OK, claimable": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}})
				// claimables
				claimable := &state.Claimable{Owner: &rewardOwner1, ExpiredDepositReward: 10, ValidatorReward: 100}
				s.EXPECT().GetClaimable(claimableOwnerID).Return(claimable, nil)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{{
					ID: claimableOwnerID, Type: txs.ClaimTypeAllTreasury, Amount: 60,
				}},
				claimTo: &rewardOwner1,
				keys: []*secp256k1.PrivateKey{
					feeKey,
					rewardOwner1Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          60,
							OutputOwners: rewardOwner1,
						},
					}}),
					Claimables: []txs.ClaimAmount{{
						ID:        claimableOwnerID,
						Type:      txs.ClaimTypeAllTreasury,
						Amount:    60,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					}},
				}, txs.Codec, [][]*secp256k1.PrivateKey{
					{feeKey},
					{rewardOwner1Key},
				})
				require.NoError(t, err)
				return tx
			},
			expectedErr: nil,
		},
		"OK, 1 claimable, 1 deposit, owner key intersects": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}, rewardOwner2Addr: {}})
				// deposits
				s.EXPECT().GetDeposit(depositTxID1).Return(&deposits.Deposit{RewardOwner: &secp256k1fx.OutputOwners{
					Threshold: 2,
					Addrs:     []ids.ShortID{rewardOwner1Addr, rewardOwner2Addr},
				}}, nil)
				// claimables
				claimable := &state.Claimable{Owner: &rewardOwner1, ExpiredDepositReward: 10, ValidatorReward: 100}
				s.EXPECT().GetClaimable(claimableOwnerID).Return(claimable, nil)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{
					{ID: depositTxID1, Type: txs.ClaimTypeActiveDepositReward, Amount: 11},
					{ID: claimableOwnerID, Type: txs.ClaimTypeAllTreasury, Amount: 60},
				},
				claimTo: &rewardOwner1,
				keys: []*secp256k1.PrivateKey{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{
						{
							Asset: avax.Asset{ID: ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt:          11,
								OutputOwners: rewardOwner1,
							},
						},
						{
							Asset: avax.Asset{ID: ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt:          60,
								OutputOwners: rewardOwner1,
							},
						},
					}),
					Claimables: []txs.ClaimAmount{
						{
							ID:        depositTxID1,
							Type:      txs.ClaimTypeActiveDepositReward,
							Amount:    11,
							OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
						},
						{
							ID:        claimableOwnerID,
							Type:      txs.ClaimTypeAllTreasury,
							Amount:    60,
							OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				}, txs.Codec, [][]*secp256k1.PrivateKey{
					{feeKey},
					{rewardOwner1Key, rewardOwner2Key},
					{rewardOwner1Key},
				})
				require.NoError(t, err)
				return tx
			},
			expectedErr: nil,
		},
		"Fail, deposit errored": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits
				s.EXPECT().GetDeposit(depositTxID1).Return(nil, database.ErrNotFound)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{{
					ID: depositTxID1, Type: txs.ClaimTypeActiveDepositReward, Amount: 11,
				}},
				claimTo: &rewardOwner1,
				keys:    []*secp256k1.PrivateKey{feeKey},
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail, deposit rewards owner isn't secp type (shouldn't happen)": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits

				s.EXPECT().GetDeposit(depositTxID1).Return(&deposits.Deposit{RewardOwner: fx.NewMockOwner(ctrl)}, nil)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{{
					ID: depositTxID1, Type: txs.ClaimTypeActiveDepositReward, Amount: 11,
				}},
				claimTo: &rewardOwner1,
				keys:    []*secp256k1.PrivateKey{feeKey},
			},
			expectedErr: errNotSECPOwner,
		},
		"Fail, missing deposit signer": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits
				s.EXPECT().GetDeposit(depositTxID1).Return(&deposits.Deposit{RewardOwner: &rewardOwner1}, nil)
				s.EXPECT().GetMultisigAlias(rewardOwner1Addr).Return(nil, database.ErrNotFound)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{{
					ID: depositTxID1, Type: txs.ClaimTypeActiveDepositReward, Amount: 11,
				}},
				claimTo: &rewardOwner1,
				keys:    []*secp256k1.PrivateKey{feeKey},
			},
			expectedErr: errKeyMissing,
		},
		"Fail, claimable errored (not found)": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// claimables
				s.EXPECT().GetClaimable(claimableOwnerID).Return(nil, database.ErrNotFound)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{{
					ID: claimableOwnerID, Type: txs.ClaimTypeAllTreasury, Amount: 1,
				}},
				claimTo: &rewardOwner1,
				keys:    []*secp256k1.PrivateKey{feeKey},
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail, missing claimable signer": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expect.Lock(t, s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// claimables
				claimable := &state.Claimable{Owner: &rewardOwner1}
				s.EXPECT().GetClaimable(claimableOwnerID).Return(claimable, nil)
				s.EXPECT().GetMultisigAlias(rewardOwner1Addr).Return(nil, database.ErrNotFound)
				return s
			},
			args: args{
				claimables: []txs.ClaimAmount{{
					ID: claimableOwnerID, Type: txs.ClaimTypeAllTreasury, Amount: 1,
				}},
				claimTo: &rewardOwner1,
				keys:    []*secp256k1.PrivateKey{feeKey},
			},
			expectedErr: errKeyMissing,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			b := newCaminoBuilder(t, tt.state(gomock.NewController(t)), nil, test.PhaseLast)

			tx, err := b.NewClaimTx(
				tt.args.claimables,
				tt.args.claimTo,
				tt.args.keys,
				tt.args.change,
			)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedTx != nil {
				require.Equal(tx, tt.expectedTx(t))
			} else {
				require.Nil(tx)
			}
		})
	}
}

func TestNewRewardsImportTx(t *testing.T) {
	ctx := test.Context(t)
	blockTime := time.Unix(1000, 0)

	tests := map[string]struct {
		state        func(*gomock.Controller) state.State
		sharedMemory func(*gomock.Controller, []*avax.TimedUTXO) atomic.SharedMemory
		utxos        []*avax.TimedUTXO
		expectedTx   func(*testing.T, []*avax.TimedUTXO) *txs.Tx
		expectedErr  error
	}{
		"OK": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				return s
			},
			sharedMemory: func(c *gomock.Controller, utxos []*avax.TimedUTXO) atomic.SharedMemory {
				shm := atomic.NewMockSharedMemory(c)
				utxosBytes := make([][]byte, len(utxos))
				for i, utxo := range utxos {
					var toMarshal interface{} = utxo
					if utxo.Timestamp == 0 {
						toMarshal = utxo.UTXO
					}
					utxoBytes, err := txs.Codec.Marshal(txs.Version, toMarshal)
					require.NoError(t, err)
					utxosBytes[i] = utxoBytes
				}
				shm.EXPECT().Indexed(ctx.CChainID, treasury.AddrTraitsBytes,
					ids.ShortEmpty[:], ids.Empty[:], MaxPageSize).Return(utxosBytes, nil, nil, nil)
				return shm
			},
			utxos: []*avax.TimedUTXO{
				{
					UTXO:      *generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty, false),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO: *generate.UTXO(ids.ID{2}, ctx.AVAXAssetID, 10, *treasury.Owner, ids.Empty, ids.Empty, false),
				},
				{
					UTXO:      *generate.UTXO(ids.ID{3}, ctx.AVAXAssetID, 100, *treasury.Owner, ids.Empty, ids.Empty, false),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO:      *generate.UTXO(ids.ID{4}, ctx.AVAXAssetID, 1000, *treasury.Owner, ids.Empty, ids.Empty, false),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound + 1,
				},
			},
			expectedTx: func(t *testing.T, utxos []*avax.TimedUTXO) *txs.Tx {
				tx, err := txs.NewSigned(&txs.RewardsImportTx{BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, &utxos[0].UTXO, []uint32{0}, false),
							generate.InFromUTXO(t, &utxos[2].UTXO, []uint32{0}, false),
						},
					},
					SyntacticallyVerified: true,
				}}, txs.Codec, nil)
				require.NoError(t, err)
				return tx
			},
		},
		"No utxos": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				return s
			},
			sharedMemory: func(c *gomock.Controller, utxos []*avax.TimedUTXO) atomic.SharedMemory {
				shm := atomic.NewMockSharedMemory(c)
				shm.EXPECT().Indexed(ctx.CChainID, treasury.AddrTraitsBytes,
					ids.ShortEmpty[:], ids.Empty[:], MaxPageSize).Return(nil, nil, nil, nil)
				return shm
			},
			expectedErr: errNoUTXOsForImport,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			b := newCaminoBuilder(t, tt.state(ctrl), tt.sharedMemory(ctrl, tt.utxos), test.PhaseLast)
			b.clk.Set(blockTime)

			tx, err := b.NewRewardsImportTx()
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedTx != nil {
				require.Equal(tt.expectedTx(t, tt.utxos), tx)
			} else {
				require.Nil(tx)
			}
		})
	}
}
