// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func generateBaseTx(assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *txs.BaseTx {
	var out avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          amount,
		OutputOwners: outputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		out = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: out,
		}
	}

	return &txs.BaseTx{
		BaseTx: avax.BaseTx{
			Outs: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{ID: assetID},
					Out:   out,
				},
			},
		},
	}
}

func newEmptyState(t *testing.T) *state {
	vdrs := validators.NewManager()
	_ = vdrs.Add(constants.PrimaryNetworkID, validators.NewSet())
	newState, err := new(
		memdb.New(),
		metrics.Noop,
		&config.Config{
			Validators: vdrs,
		},
		&snow.Context{},
		prometheus.NewRegistry(),
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
		&utils.Atomic[bool]{},
	)
	require.NoError(t, err)
	require.NotNil(t, newState)
	return newState
}

func newMockStateVersions(c *gomock.Controller, parentStateID ids.ID, parentState Chain) *MockVersions {
	stateVersions := NewMockVersions(c)
	stateVersions.EXPECT().GetState(parentStateID).Return(parentState, true)
	return stateVersions
}

func generateTestUTXO(txID ids.ID, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *avax.UTXO { //nolint:unparam
	return generateTestUTXOWithIndex(txID, 0, assetID, amount, outputOwners, depositTxID, bondTxID, true)
}

func generateTestUTXOWithIndex(txID ids.ID, outIndex uint32, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID, init bool) *avax.UTXO {
	var out avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          amount,
		OutputOwners: outputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		out = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: out,
		}
	}
	testUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: outIndex,
		},
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
	if init {
		testUTXO.InputID()
	}
	return testUTXO
}
