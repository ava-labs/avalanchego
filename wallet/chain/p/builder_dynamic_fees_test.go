// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

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

	be := NewMockBuilderBackend(ctrl)

	var (
		subnetAuthKey  = testKeys[0]
		utxoKey        = testKeys[1]
		subnetAuthAddr = subnetAuthKey.PublicKey().Address()
		utxoAddr       = utxoKey.PublicKey().Address()
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
		amount      = 10 * units.Avax
		avaxAssetID = ids.GenerateTestID()
		utxos       = []*avax.UTXO{
			{
				UTXOID: avax.UTXOID{
					TxID:        ids.GenerateTestID(),
					OutputIndex: rand.Uint32(), // #nosec G404
				},
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Addrs:     []ids.ShortID{utxoKey.PublicKey().Address()},
						Threshold: 1,
					},
				},
			},
		}
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
		kc  = secp256k1fx.NewKeychain(utxoKey)
		sbe = NewMockSignerBackend(ctrl)
		s   = NewSigner(kc, sbe)
	)

	sbe.EXPECT().GetUTXO(gomock.Any(), gomock.Any(), gomock.Any()).Return(utxos[0], nil)
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
	require.Equal(3197*units.MicroAvax, fc.Fee)

	ins := utx.Ins
	outs := utx.Outs
	require.Len(ins, 1)
	require.Len(outs, 1)
	require.Equal(fc.Fee, ins[0].In.Amount()-outs[0].Out.Amount())
}
