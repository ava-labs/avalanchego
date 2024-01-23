// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var testKeys = secp256k1.TestKeys()

func TestCreateChainTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	be := NewMockBuilderBackend(ctrl)

	var (
		utxoKey  = testKeys[0]
		utxoAddr = utxoKey.PublicKey().Address()
	)

	b := &DynamicFeesBuilder{
		addrs:   set.Of(utxoAddr),
		backend: be,
	}

	var (
		subnetID     = ids.GenerateTestID()
		genesisBytes = []byte{'a', 'b', 'c'}
		vmID         = ids.GenerateTestID()
		fxIDs        = []ids.ID{ids.GenerateTestID()}
		chainName    = "dummyChain"
	)

	var (
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

	be.EXPECT().GetTx(gomock.Any(), subnetID).Return(
		&txs.Tx{
			Unsigned: &txs.CreateSubnetTx{
				Owner: &secp256k1fx.OutputOwners{},
			},
		},
		nil,
	)

	amount := 10 * units.Avax
	avaxAssetID := ids.GenerateTestID()
	be.EXPECT().AVAXAssetID().Return(avaxAssetID).AnyTimes()
	be.EXPECT().NetworkID().Return(constants.MainnetID).AnyTimes()

	be.EXPECT().UTXOs(gomock.Any(), constants.PlatformChainID).Return(
		[]*avax.UTXO{
			&avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        ids.GenerateTestID(),
					OutputIndex: rand.Uint32(),
				},
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(amount),
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
						Threshold: 1,
					},
				},
			},
		},
		nil,
	)

	_, err := b.NewCreateChainTx(
		subnetID,
		genesisBytes,
		vmID,
		fxIDs,
		chainName,
		testUnitFees,
		testBlockMaxConsumedUnits,
	)
	require.NoError(err)

	// TODO: sign and verify fees can be paid
}
