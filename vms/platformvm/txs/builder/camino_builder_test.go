// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
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
			_, err := env.txBuilder.NewAddAddressStateTx(
				tt.address,
				tt.remove,
				tt.state,
				caminoPreFundedKeys,
				ids.ShortEmpty,
			)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoBuilderNewAddValidatorTxNodeSig(t *testing.T) {
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
			expectedErr: errNodeKeyMissing,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit true, VerifyNodeSignature true": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			nodeID:      nodeID1,
			nodeKey:     nodeKey2,
			expectedErr: errNodeKeyMissing,
		},
		// No need to add tests with VerifyNodeSignature set to false
		// because the error will rise from the execution
	}
	for name, tt := range tests {
		t.Run("AddValidatorTx: "+name, func(t *testing.T) {
			env := newCaminoEnvironment(true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			_, err := env.txBuilder.NewAddValidatorTx(
				defaultCaminoValidatorWeight,
				uint64(defaultValidateStartTime.Unix()+1),
				uint64(defaultValidateEndTime.Unix()),
				tt.nodeID,
				ids.ShortEmpty,
				reward.PercentDenominator,
				[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], tt.nodeKey},
				ids.ShortEmpty,
			)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("AddSubnetValidatorTx: "+name, func(t *testing.T) {
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
				ids.ShortEmpty,
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
