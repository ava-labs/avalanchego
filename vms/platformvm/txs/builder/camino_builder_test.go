// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"math"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
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
		DepositOffers: []genesis.DepositOffer{
			{
				UnlockPeriodDuration:  60,
				InterestRateNominator: 0,
				Start:                 uint64(time.Now().Add(-60 * time.Hour).Unix()),
				End:                   uint64(time.Now().Add(+60 * time.Hour).Unix()),
				MinAmount:             1,
				MinDuration:           60,
				MaxDuration:           60,
			},
		},
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
			env.state.UpdateDeposit(depositTxID, deposit)
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

func TestNewClaimRewardTx(t *testing.T) {
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()

	feeKey, feeAddr, feeUTXOOwner := generateKeyAndOwner()
	rewardOwner1Key, rewardOwner1Addr, rewardOwner1 := generateKeyAndOwner()
	rewardOwner2Key, rewardOwner2Addr, rewardOwner2 := generateKeyAndOwner()

	feeUTXO := generateTestUTXO(ids.ID{1}, avaxAssetID, defaultTxFee, feeUTXOOwner, ids.Empty, ids.Empty)

	type args struct {
		depositTxIDs  []ids.ID
		rewardAddress ids.ShortID
		keys          []*crypto.PrivateKeySECP256K1R
		change        *secp256k1fx.OutputOwners
	}

	tests := map[string]struct {
		state           func(*gomock.Controller) state.State
		args            args
		expectedSigsLen int
		expectedErr     error
	}{
		"OK, single deposit tx": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().UTXOIDs(rewardOwner1Addr[:], ids.Empty, math.MaxInt).Return([]ids.ID{}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
				// deposits
				depositTx := &txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner1}}
				s.EXPECT().GetTx(depositTxID1).Return(depositTx, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID1).Return(depositTx, status.Committed, nil)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1},
				rewardAddress: rewardOwner1Addr,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
				},
			},
			expectedSigsLen: 1,
			expectedErr:     nil,
		},
		"OK, two deposit tx": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().UTXOIDs(rewardOwner1Addr[:], ids.Empty, math.MaxInt).Return([]ids.ID{}, nil)
				s.EXPECT().UTXOIDs(rewardOwner2Addr[:], ids.Empty, math.MaxInt).Return([]ids.ID{}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
				// deposits
				depositTx1 := &txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner1}}
				depositTx2 := &txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner2}}
				s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID2).Return(depositTx2, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID2).Return(depositTx2, status.Committed, nil)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1, depositTxID2},
				rewardAddress: rewardOwner1Addr,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedSigsLen: 2,
			expectedErr:     nil,
		},
		"OK, two deposit tx, owner keys intersect": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().UTXOIDs(rewardOwner1Addr[:], ids.Empty, math.MaxInt).Return([]ids.ID{}, nil)
				s.EXPECT().UTXOIDs(rewardOwner2Addr[:], ids.Empty, math.MaxInt).Return([]ids.ID{}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
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
				s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID2).Return(depositTx2, status.Committed, nil)
				s.EXPECT().GetTx(depositTxID2).Return(depositTx2, status.Committed, nil)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1, depositTxID2},
				rewardAddress: rewardOwner1Addr,
				keys: []*crypto.PrivateKeySECP256K1R{
					feeKey,
					rewardOwner1Key,
					rewardOwner2Key,
				},
			},
			expectedSigsLen: 2,
			expectedErr:     nil,
		},
		"Fail, deposit errored": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(nil, status.Unknown, database.ErrNotFound)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1},
				rewardAddress: rewardOwner1Addr,
				keys:          []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail, deposit not committed": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(nil, status.Unknown, nil)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1},
				rewardAddress: rewardOwner1Addr,
				keys:          []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errTxIsNotCommitted,
		},
		"Fail, deposit isn't deposit": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{}},
					status.Committed,
					nil,
				)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1},
				rewardAddress: rewardOwner1Addr,
				keys:          []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errWrongTxType,
		},
		"Fail, deposit rewards owner isn't secp type (shouldn't happen)": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &avax.TransferableOutput{}}},
					status.Committed,
					nil,
				)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1},
				rewardAddress: rewardOwner1Addr,
				keys:          []*crypto.PrivateKeySECP256K1R{feeKey},
			},
			expectedErr: errNotSECPOwner,
		},
		"Fail, missing deposit signer": {
			state: func(ctrl *gomock.Controller) state.State {
				s := state.NewMockState(ctrl)
				// utxo fetching for utxo handler Lock(..)
				s.EXPECT().UTXOIDs(feeAddr[:], ids.Empty, math.MaxInt).Return([]ids.ID{feeUTXO.InputID()}, nil)
				s.EXPECT().GetUTXO(feeUTXO.InputID()).Return(feeUTXO, nil)
				// fee utxo
				s.EXPECT().GetMultisigAlias(feeAddr).Return(nil, database.ErrNotFound)
				// deposits
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &rewardOwner1}},
					status.Committed,
					nil,
				)
				return s
			},
			args: args{
				depositTxIDs:  []ids.ID{depositTxID1},
				rewardAddress: rewardOwner1Addr,
				keys:          []*crypto.PrivateKeySECP256K1R{feeKey},
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

			// testing

			tx, err := b.NewClaimRewardTx(
				tt.args.depositTxIDs,
				tt.args.rewardAddress,
				tt.args.keys,
				tt.args.change,
			)
			require.ErrorIs(err, tt.expectedErr)

			if tt.expectedErr != nil {
				return
			}

			// checking tx

			utx, ok := tx.Unsigned.(*txs.ClaimRewardTx)
			require.True(ok)
			require.Len(tx.Creds, 2)
			require.Equal(tt.args.depositTxIDs, utx.DepositTxs)
			require.Equal(&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{tt.args.rewardAddress},
			}, utx.RewardsOwner)

			txHash := hashing.ComputeHash256(tx.Unsigned.Bytes())

			// checking input creds

			require.Len(utx.Ins, 1)
			feeInput, ok := utx.Ins[0].In.(*secp256k1fx.TransferInput)
			require.True(ok)
			err = b.fx.VerifyTransfer(utx, feeInput, tx.Creds[0], feeUTXO.Out)
			require.NoError(err)

			// checking deposit cred

			depositCred, ok := tx.Creds[1].(*secp256k1fx.Credential)
			require.True(ok)
			require.Len(depositCred.Sigs, tt.expectedSigsLen)

			signedAddresses := set.Set[ids.ShortID]{}

			// gather addresses and check that they are unique
			for i := range depositCred.Sigs {
				pk, err := testKeyfactory.RecoverHashPublicKey(txHash, depositCred.Sigs[i][:])
				require.NoError(err)
				addr := pk.Address()
				require.False(signedAddresses.Contains(addr))
				signedAddresses.Add(addr)
			}

			for _, depositTxID := range tt.args.depositTxIDs {
				depositRewardsOwner, err := getDepositRewardsOwner(b.state, depositTxID)
				require.NoError(err)
				err = b.fx.VerifyPermissionUnordered(
					tx.Unsigned,
					tx.Creds[1],
					depositRewardsOwner,
				)
				require.NoError(err)
			}
		})
	}
}
