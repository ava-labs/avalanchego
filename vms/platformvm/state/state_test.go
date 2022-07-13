// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	initialTxID             = ids.GenerateTestID()
	initialNodeID           = ids.GenerateTestNodeID()
	initialTime             = time.Now().Round(time.Second)
	initialValidatorEndTime = initialTime.Add(28 * 24 * time.Hour)
)

func TestStateInitialization(t *testing.T) {
	assert := assert.New(t)
	state, db := newUninitializedState(assert)

	shouldInit, err := state.ShouldInit()
	assert.NoError(err)
	assert.True(shouldInit)

	assert.NoError(state.DoneInit())

	state = newStateFromDB(assert, db)

	shouldInit, err = state.ShouldInit()
	assert.NoError(err)
	assert.False(shouldInit)
}

func TestStateSyncGenesis(t *testing.T) {
	assert := assert.New(t)
	state, _ := newInitializedState(assert)

	staker, err := state.GetCurrentValidator(constants.PrimaryNetworkID, initialNodeID)
	assert.NoError(err)
	assert.NotNil(staker)
	assert.Equal(initialNodeID, staker.NodeID)

	delegatorIterator, err := state.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, initialNodeID)
	assert.NoError(err)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)

	stakerIterator, err := state.GetCurrentStakerIterator()
	assert.NoError(err)
	assertIteratorsEqual(t, NewSliceIterator(staker), stakerIterator)

	_, err = state.GetPendingValidator(constants.PrimaryNetworkID, initialNodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	delegatorIterator, err = state.GetPendingDelegatorIterator(constants.PrimaryNetworkID, initialNodeID)
	assert.NoError(err)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)
}

func newInitializedState(assert *assert.Assertions) (State, database.Database) {
	state, db := newUninitializedState(assert)

	initialValidator := &txs.AddValidatorTx{
		Validator: validator.Validator{
			NodeID: initialNodeID,
			Start:  uint64(initialTime.Unix()),
			End:    uint64(initialValidatorEndTime.Unix()),
			Wght:   units.Avax,
		},
		Stake: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: initialTxID},
				Out: &secp256k1fx.TransferOutput{
					Amt: units.Avax,
				},
			},
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
		Shares:       reward.PercentDenominator,
	}
	initialValidatorTx := &txs.Tx{Unsigned: initialValidator}
	assert.NoError(initialValidatorTx.Sign(txs.Codec, nil))

	initialChain := &txs.CreateChainTx{
		SubnetID:   constants.PrimaryNetworkID,
		ChainName:  "x",
		VMID:       constants.AVMID,
		SubnetAuth: &secp256k1fx.Input{},
	}
	initialChainTx := &txs.Tx{Unsigned: initialChain}
	assert.NoError(initialChainTx.Sign(txs.Codec, nil))

	genesisBlkID := ids.GenerateTestID()
	genesisState := &genesis.State{
		UTXOs: []*avax.UTXO{
			{
				UTXOID: avax.UTXOID{
					TxID:        initialTxID,
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: initialTxID},
				Out: &secp256k1fx.TransferOutput{
					Amt: units.Schmeckle,
				},
			},
		},
		Validators: []*txs.Tx{
			initialValidatorTx,
		},
		Chains: []*txs.Tx{
			initialChainTx,
		},
		Timestamp:     uint64(initialTime.Unix()),
		InitialSupply: units.Schmeckle + units.Avax,
	}
	assert.NoError(state.SyncGenesis(genesisBlkID, genesisState))

	return state, db
}

func newUninitializedState(assert *assert.Assertions) (State, database.Database) {
	db := memdb.New()
	return newStateFromDB(assert, db), db
}

func newStateFromDB(assert *assert.Assertions, db database.Database) State {
	vdrs := validators.NewManager()
	assert.NoError(vdrs.Set(constants.PrimaryNetworkID, validators.NewSet()))

	state, err := New(
		db,
		prometheus.NewRegistry(),
		&config.Config{
			Validators: vdrs,
		},
		&snow.Context{},
		prometheus.NewGauge(prometheus.GaugeOpts{}),
		prometheus.NewGauge(prometheus.GaugeOpts{}),
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
	)
	assert.NoError(err)
	assert.NotNil(state)
	return state
}
