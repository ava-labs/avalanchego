// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	deposits "github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

func TestCaminoEnv(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ false, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		err := shutdownCaminoEnvironment(env)
		require.NoError(t, err)
	}()
	env.config.BanffTime = env.state.GetTimestamp()
}

func TestCaminoBuilderTxAddressState(t *testing.T) {
	caminoConfig := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	env := newCaminoEnvironment(true, caminoConfig)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownCaminoEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	tests := map[string]struct {
		remove      bool
		state       uint8
		address     ids.ShortID
		expectedErr error
	}{
		"KYC Role: Add": {
			remove:      false,
			state:       txs.AddressStateRoleKyc,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"KYC Role: Remove": {
			remove:      true,
			state:       txs.AddressStateRoleKyc,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"Admin Role: Add": {
			remove:      false,
			state:       txs.AddressStateRoleAdmin,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"Admin Role: Remove": {
			remove:      true,
			state:       txs.AddressStateRoleAdmin,
			address:     caminoPreFundedKeys[0].PublicKey().Address(),
			expectedErr: nil,
		},
		"Empty Address": {
			remove:      false,
			state:       txs.AddressStateRoleKyc,
			address:     ids.ShortEmpty,
			expectedErr: txs.ErrEmptyAddress,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := env.txBuilder.NewAddressStateTx(
				tt.address,
				tt.remove,
				tt.state,
				caminoPreFundedKeys,
				nil,
			)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoBuilderNewAddSubnetValidatorTxNodeSig(t *testing.T) {
	nodeKey1, nodeID1 := nodeid.GenerateCaminoNodeKeyAndID()
	nodeKey2, _ := nodeid.GenerateCaminoNodeKeyAndID()

	tests := map[string]struct {
		caminoConfig api.Camino
		nodeID       ids.NodeID
		nodeKey      *crypto.PrivateKeySECP256K1R
		expectedErr  error
	}{
		"Happy path, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:      nodeID1,
			nodeKey:     nodeKey1,
			expectedErr: nil,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:      nodeID1,
			nodeKey:     nodeKey2,
			expectedErr: errKeyMissing,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit true, VerifyNodeSignature true": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			nodeID:      nodeID1,
			nodeKey:     nodeKey2,
			expectedErr: errKeyMissing,
		},
		// No need to add tests with VerifyNodeSignature set to false
		// because the error will rise from the execution
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := newCaminoEnvironment(true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			_, err := env.txBuilder.NewAddSubnetValidatorTx(
				defaultCaminoValidatorWeight,
				uint64(defaultValidateStartTime.Unix()+1),
				uint64(defaultValidateEndTime.Unix()),
				tt.nodeID,
				testSubnet1.ID(),
				[]*crypto.PrivateKeySECP256K1R{testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], tt.nodeKey},
				ids.ShortEmpty,
			)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestUnlockDepositTx(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		DepositOffers: []*deposits.Offer{{
			UnlockPeriodDuration:  60,
			InterestRateNominator: 0,
			Start:                 uint64(time.Now().Add(-60 * time.Hour).Unix()),
			End:                   uint64(time.Now().Add(+60 * time.Hour).Unix()),
			MinAmount:             1,
			MinDuration:           60,
			MaxDuration:           60,
		}},
	}
	testKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{testKey.PublicKey().Address()},
	}
	depositTxID := ids.GenerateTestID()
	depositStartTime := time.Now()
	depositExpiredTime := depositStartTime.Add(100 * time.Second)
	deposit := &deposits.Deposit{
		Duration: 60,
		Amount:   defaultCaminoValidatorWeight,
		Start:    uint64(depositStartTime.Unix()),
	}

	tests := map[string]struct {
		utxos       []*avax.UTXO
		expectedErr error
	}{
		"Happy path, ins and feeIns consumed different UTXOs": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			expectedErr: nil,
		},
		"Happy path, multiple ins and multiple feeIns consumed different UTXOs": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{3}, avaxAssetID, defaultTxFee/2, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{4}, avaxAssetID, defaultTxFee/2, outputOwners, ids.Empty, ids.Empty),
			},
			expectedErr: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
			env.ctx.Lock.Lock()
			defer func() {
				if err = shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()
			env.state.SetTimestamp(depositStartTime)
			genesisOffers, err := env.state.GetAllDepositOffers()
			require.NoError(t, err)

			// Add a deposit to state
			deposit.DepositOfferID = genesisOffers[0].ID
			env.state.SetDeposit(depositTxID, deposit)
			err = env.state.Commit()
			require.NoError(t, err)
			env.clk.Set(depositExpiredTime)

			// Add utxos to state
			for _, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
			}
			err = env.state.Commit()
			require.NoError(t, err)

			tx, err := env.txBuilder.NewUnlockDepositTx(
				[]ids.ID{depositTxID},
				[]*crypto.PrivateKeySECP256K1R{testKey.(*crypto.PrivateKeySECP256K1R)},
				nil,
			)
			require.ErrorIs(t, err, tt.expectedErr)

			consumedUTXOIDs := make(map[ids.ID]bool)
			utx := tx.Unsigned.(*txs.UnlockDepositTx)
			ins := utx.Ins
			for _, in := range ins {
				require.False(t, consumedUTXOIDs[in.InputID()])
				consumedUTXOIDs[in.InputID()] = true
			}
		})
	}
}

func TestNewClaimTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)

	caminoConfig := &state.CaminoConfig{
		LockModeBondDeposit: true,
	}

	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()

	feeKey, feeAddr, feeUTXOOwner := generateKeyAndOwner()
	rewardOwner1Key, rewardOwner1Addr, rewardOwner1 := generateKeyAndOwner()
	rewardOwner2Key, rewardOwner2Addr, rewardOwner2 := generateKeyAndOwner()
	claimableOwnerID := ids.GenerateTestID()

	feeUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, feeUTXOOwner, ids.Empty, ids.Empty)
	treasuryUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, 110, *treasury.Owner, ids.Empty, ids.Empty)

	baseTx := txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: feeUTXO.UTXOID,
				Asset:  feeUTXO.Asset,
				In: &secp256k1fx.TransferInput{
					Amt:   defaultTxFee,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
			Outs: []*avax.TransferableOutput{},
		},
		SyntacticallyVerified: true,
	}

	type args struct {
		depositTxIDs      []ids.ID
		claimableOwnerIDs []ids.ID
		amountToClaim     []uint64
		claimTo           *secp256k1fx.OutputOwners
		keys              []*crypto.PrivateKeySECP256K1R
		change            *secp256k1fx.OutputOwners
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
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}})
				// deposits
				depositTx := &txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner1}}
				s.EXPECT().GetTx(depositTxID1).Return(depositTx, status.Committed, nil)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1},
				claimTo:      &rewardOwner1,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx:              baseTx,
					DepositTxs:          []ids.ID{depositTxID1},
					DepositRewardsOwner: &rewardOwner1,
				}, txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{feeKey}, {rewardOwner1Key}})
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
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}, rewardOwner2Addr: {}})
				// deposits
				depositTx1 := &txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner1}}
				depositTx2 := &txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner2}}
				s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID2).Return(depositTx2, status.Committed, nil)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1, depositTxID2},
				claimTo:      &rewardOwner1,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx:              baseTx,
					DepositTxs:          []ids.ID{depositTxID1, depositTxID2},
					DepositRewardsOwner: &rewardOwner1,
				}, txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{feeKey}, {rewardOwner1Key, rewardOwner2Key}})
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
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}, rewardOwner2Addr: {}})
				// deposits
				depositTx1 := &txs.Tx{Unsigned: &txs.DepositTx{
					RewardsOwner: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs: []ids.ShortID{
							rewardOwner1Key.Address(),
							rewardOwner2Key.Address(),
						},
					},
				}}
				depositTx2 := &txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner1}}
				s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID2).Return(depositTx2, status.Committed, nil)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1, depositTxID2},
				claimTo:      &rewardOwner1,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx:              baseTx,
					DepositTxs:          []ids.ID{depositTxID1, depositTxID2},
					DepositRewardsOwner: &rewardOwner1,
				}, txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{feeKey}, {rewardOwner1Key, rewardOwner2Key}})
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
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}})
				// claimables
				claimable := &state.Claimable{Owner: &rewardOwner1, DepositReward: 10, ValidatorReward: 100}
				s.EXPECT().GetClaimable(claimableOwnerID).Return(claimable, nil)
				expectLock(s, map[ids.ShortID][]*avax.UTXO{treasury.Addr: {treasuryUTXO}})
				return s
			},
			args: args{
				claimableOwnerIDs: []ids.ID{claimableOwnerID},
				amountToClaim:     []uint64{60},
				claimTo:           &rewardOwner1,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx:            baseTx,
					ClaimableOwnerIDs: []ids.ID{claimableOwnerID},
					ClaimedAmount:     []uint64{60},
					ClaimableIns: []*avax.TransferableInput{
						generateTestInFromUTXO(treasuryUTXO, []uint32{0}),
					},
					ClaimableOuts: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 50, *treasury.Owner, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 60, rewardOwner1, ids.Empty, ids.Empty),
					},
					DepositRewardsOwner: &rewardOwner1,
				}, txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{feeKey}, {rewardOwner1Key}})
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
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}, rewardOwner1Addr: {}, rewardOwner2Addr: {}})
				// deposits
				depositTx1 := &txs.Tx{Unsigned: &txs.DepositTx{
					RewardsOwner: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs:     []ids.ShortID{rewardOwner1Addr, rewardOwner2Addr},
					},
				}}
				s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				// claimables
				claimable := &state.Claimable{Owner: &rewardOwner1, DepositReward: 10, ValidatorReward: 100}
				s.EXPECT().GetClaimable(claimableOwnerID).Return(claimable, nil)
				expectLock(s, map[ids.ShortID][]*avax.UTXO{treasury.Addr: {treasuryUTXO}})
				return s
			},
			args: args{
				depositTxIDs:      []ids.ID{depositTxID1},
				claimableOwnerIDs: []ids.ID{claimableOwnerID},
				amountToClaim:     []uint64{60},
				claimTo:           &rewardOwner1,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedTx: func(t *testing.T) *txs.Tx {
				tx, err := txs.NewSigned(&txs.ClaimTx{
					BaseTx:            baseTx,
					DepositTxs:        []ids.ID{depositTxID1},
					ClaimableOwnerIDs: []ids.ID{claimableOwnerID},
					ClaimedAmount:     []uint64{60},
					ClaimableIns: []*avax.TransferableInput{
						generateTestInFromUTXO(treasuryUTXO, []uint32{0}),
					},
					ClaimableOuts: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 50, *treasury.Owner, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 60, rewardOwner1, ids.Empty, ids.Empty),
					},
					DepositRewardsOwner: &rewardOwner1,
				}, txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{feeKey}, {rewardOwner1Key, rewardOwner2Key}})
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
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(nil, status.Unknown, database.ErrNotFound)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1},
				claimTo:      &rewardOwner1,
				keys:         []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail, deposit not committed": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(nil, status.Unknown, nil)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1},
				claimTo:      &rewardOwner1,
				keys:         []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errTxIsNotCommitted,
		},
		"Fail, deposit isn't deposit": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{}},
					status.Committed,
					nil,
				)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1},
				claimTo:      &rewardOwner1,
				keys:         []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errWrongTxType,
		},
		"Fail, deposit rewards owner isn't secp type (shouldn't happen)": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &avax.TransferableOutput{}}},
					status.Committed,
					nil,
				)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1},
				claimTo:      &rewardOwner1,
				keys:         []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errNotSECPOwner,
		},
		"Fail, missing deposit signer": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner1}},
					status.Committed,
					nil,
				)
				return s
			},
			args: args{
				depositTxIDs: []ids.ID{depositTxID1},
				claimTo:      &rewardOwner1,
				keys:         []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errKeyMissing,
		},
		"Fail, claimable errored (not found)": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// claimables
				s.EXPECT().GetClaimable(claimableOwnerID).Return(nil, database.ErrNotFound)
				return s
			},
			args: args{
				claimableOwnerIDs: []ids.ID{claimableOwnerID},
				amountToClaim:     []uint64{1},
				claimTo:           &rewardOwner1,
				keys:              []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail, missing claimable signer": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				s.EXPECT().CaminoConfig().Return(caminoConfig, nil)
				// fee
				expectLock(s, map[ids.ShortID][]*avax.UTXO{feeAddr: {feeUTXO}})
				// claimables
				claimable := &state.Claimable{Owner: &rewardOwner1}
				s.EXPECT().GetClaimable(claimableOwnerID).Return(claimable, nil)
				return s
			},
			args: args{
				claimableOwnerIDs: []ids.ID{claimableOwnerID},
				amountToClaim:     []uint64{1},
				claimTo:           &rewardOwner1,
				keys:              []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errKeyMissing,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			b, db := newCaminoBuilder(true, tt.state(ctrl))
			defer func() {
				require.NoError(db.Close())
				ctrl.Finish()
			}()

			tx, err := b.NewClaimTx(
				tt.args.depositTxIDs,
				tt.args.claimableOwnerIDs,
				tt.args.amountToClaim,
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
	ctx, _ := defaultCtx(nil)
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
				utxoIDs := make([][]byte, len(utxos))
				utxosBytes := make([][]byte, len(utxos))
				for i, utxo := range utxos {
					var toMarshal interface{} = utxo
					if utxo.Timestamp == 0 {
						toMarshal = utxo.UTXO
					}
					utxoID := utxo.InputID()
					utxoIDs[i] = utxoID[:]
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
					UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO: *generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, 10, *treasury.Owner, ids.Empty, ids.Empty),
				},
				{
					UTXO:      *generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, 100, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO:      *generateTestUTXO(ids.ID{4}, ctx.AVAXAssetID, 1000, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound + 1,
				},
			},
			expectedTx: func(t *testing.T, utxos []*avax.TimedUTXO) *txs.Tx {
				tx, err := txs.NewSigned(&txs.RewardsImportTx{BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(&utxos[0].UTXO, []uint32{0}),
							generateTestInFromUTXO(&utxos[2].UTXO, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, 101, *treasury.Owner, ids.Empty, ids.Empty),
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
			b, db := newCaminoBuilderWithMocks(true, tt.state(ctrl), tt.sharedMemory(ctrl, tt.utxos))
			defer func() {
				require.NoError(db.Close())
				ctrl.Finish()
			}()
			b.clk.Set(blockTime)

			tx, err := b.NewRewardsImportTx()
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedTx != nil {
				require.Equal(tx, tt.expectedTx(t, tt.utxos))
			} else {
				require.Nil(tx)
			}
		})
	}
}
