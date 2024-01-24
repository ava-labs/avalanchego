// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/mocks"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var (
	testKeys     = secp256k1.TestKeys()
	testUnitFees = commonfees.Dimensions{
		1 * units.MicroAvax,
		2 * units.MicroAvax,
		3 * units.MicroAvax,
		4 * units.MicroAvax,
	}
	testBlockMaxConsumedUnits = commonfees.Dimensions{
		math.MaxUint64,
		math.MaxUint64,
		math.MaxUint64,
		math.MaxUint64,
	}
)

// Create and sign the tx, then verify that utxos included
// in the tx are exactly necessary to pay fees for it
func TestCreateChainTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey  = testKeys[0]
		utxosKey       = testKeys[1]
		subnetAuthAddr = subnetAuthKey.PublicKey().Address()
		utxoAddr       = utxosKey.PublicKey().Address()
	)

	b := &DynamicFeesBuilder{
		addrs:   set.Of(utxoAddr, subnetAuthAddr),
		backend: be,
	}

	var (
		subnetID     = ids.GenerateTestID()
		genesisBytes = []byte{'a', 'b', 'c'}
		vmID         = ids.GenerateTestID()
		fxIDs        = []ids.ID{ids.GenerateTestID()}
		chainName    = "dummyChain"
	)

	subnetTx := &txs.Tx{
		Unsigned: &txs.CreateSubnetTx{
			Owner: &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{subnetAuthKey.PublicKey().Address()},
			},
		},
	}
	be.EXPECT().GetTx(gomock.Any(), subnetID).Return(subnetTx, nil)

	var (
		avaxAssetID = ids.GenerateTestID()
		utxos       = testUTXOsList(avaxAssetID, utxosKey)
	)

	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil)

	utx, err := b.NewCreateChainTx(
		subnetID,
		genesisBytes,
		vmID,
		fxIDs,
		chainName,
		testUnitFees,
		testBlockMaxConsumedUnits,
	)
	require.NoError(err)

	var (
		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}
	sbe.EXPECT().GetTx(gomock.Any(), subnetID).Return(subnetTx, nil)

	tx, err := s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	fc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       commonfees.NewManager(testUnitFees),
		ConsumedUnitsCap: testBlockMaxConsumedUnits,
		Credentials:      tx.Creds,
	}
	require.NoError(utx.Visit(fc))
	require.Equal(5808*units.MicroAvax, fc.Fee)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 2)
	require.Len(outs, 1)
	require.Equal(fc.Fee, ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
}

func TestCreateSubnetTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey  = testKeys[0]
		utxosKey       = testKeys[1]
		subnetAuthAddr = subnetAuthKey.PublicKey().Address()
		utxoAddr       = utxosKey.PublicKey().Address()
	)

	b := &DynamicFeesBuilder{
		addrs:   set.Of(utxoAddr, subnetAuthAddr),
		backend: be,
	}

	var (
		avaxAssetID = ids.GenerateTestID()
		utxos       = testUTXOsList(avaxAssetID, utxosKey)

		subnetOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				subnetAuthKey.Address(),
			},
		}
	)

	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil)

	utx, err := b.NewCreateSubnetTx(
		subnetOwner,
		testUnitFees,
		testBlockMaxConsumedUnits,
	)
	require.NoError(err)

	var (
		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	tx, err := s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	fc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       commonfees.NewManager(testUnitFees),
		ConsumedUnitsCap: testBlockMaxConsumedUnits,
		Credentials:      tx.Creds,
	}
	require.NoError(utx.Visit(fc))
	require.Equal(5644*units.MicroAvax, fc.Fee)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 2)
	require.Len(outs, 1)
	require.Equal(fc.Fee, ins[0].In.Amount()+ins[1].In.Amount()-outs[0].Out.Amount())
}

func TestAddValidatorTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := mocks.NewMockBuilderBackend(ctrl)

	var (
		rewardKey      = testKeys[0]
		utxosKey       = testKeys[1]
		subnetAuthAddr = rewardKey.PublicKey().Address()
		utxoAddr       = utxosKey.PublicKey().Address()
	)

	b := &DynamicFeesBuilder{
		addrs:   set.Of(utxoAddr, subnetAuthAddr),
		backend: be,
	}

	var (
		avaxAssetID = ids.GenerateTestID()
		utxos       = testUTXOsList(avaxAssetID, utxosKey)

		rewardOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				rewardKey.Address(),
			},
		}
	)

	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()
	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(utxos, nil)

	utx, err := b.NewAddValidatorTx(
		&txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			End:    uint64(time.Now().Add(time.Hour).Unix()),
			Wght:   2 * units.Avax,
		},
		rewardOwner,
		reward.PercentDenominator,
		testUnitFees,
		testBlockMaxConsumedUnits,
	)
	require.NoError(err)

	var (
		kc  = secp256k1fx.NewKeychain(utxosKey)
		sbe = mocks.NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	for _, utxo := range utxos {
		sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), utxo.InputID()).Return(utxo, nil).AnyTimes()
	}

	tx, err := s.SignUnsigned(stdcontext.Background(), utx)
	require.NoError(err)

	fc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       commonfees.NewManager(testUnitFees),
		ConsumedUnitsCap: testBlockMaxConsumedUnits,
		Credentials:      tx.Creds,
	}
	require.NoError(utx.Visit(fc))
	require.Equal(12184*units.MicroAvax, fc.Fee)

	ins := utx.Ins
	staked := utx.StakeOuts
	outs := utx.Outs
	require.Len(ins, 4)
	require.Len(staked, 2)
	require.Len(outs, 2)
	require.Equal(utx.Validator.Weight(), staked[0].Out.Amount()+staked[1].Out.Amount())
	require.Equal(fc.Fee, ins[2].In.Amount()+ins[3].In.Amount()-outs[0].Out.Amount())
}

func testUTXOsList(avaxAssetID ids.ID, utxosKey *secp256k1.PrivateKey) []*avax.UTXO {
	// Note: we avoid ids.GenerateTestNodeID here to make sure that UTXO IDs won't change
	// run by run. This simplifies checking what utxos are included in the built txs.
	utxosOffset := uint64(2024)

	return []*avax.UTXO{ // currently, the wallet scans UTXOs in the order provided here
		{ // a small UTXO first, which  should not be enough to pay fees
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset),
				OutputIndex: uint32(utxosOffset),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 2 * units.MilliAvax,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
					Threshold: 1,
				},
			},
		},
		{ // a locked, small UTXO
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset + 1),
				OutputIndex: uint32(utxosOffset + 1),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &stakeable.LockOut{
				Locktime: uint64(time.Now().Add(time.Second).Unix()),
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt: 3 * units.MilliAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
					},
				},
			},
		},
		{ // a locked, large UTXO
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset + 2),
				OutputIndex: uint32(utxosOffset + 2),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &stakeable.LockOut{
				Locktime: uint64(time.Now().Add(time.Second).Unix()),
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt: 99 * units.Avax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
					},
				},
			},
		},
		{ // a large UTXO last, which should be enough to pay any fee by itself
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset + 3),
				OutputIndex: uint32(utxosOffset + 3),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 10 * units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{utxosKey.PublicKey().Address()},
					Threshold: 1,
				},
			},
		},
	}
}
