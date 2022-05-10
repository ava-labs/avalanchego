// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/metadata"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/prometheus/client_golang/prometheus"
)

type Mutable interface {
	transactions.Mutable
	metadata.Mutable
}

type Content interface {
	transactions.Content
	metadata.Content
}

type State interface {
	Content

	SyncGenesis(
		genesisBlkID ids.ID,
		genesisTimestamp uint64,
		genesisInitialSupply uint64,
		genesisUtxos []*avax.UTXO,
		genesisValidator []*signed.Tx,
		genesisChains []*signed.Tx,
	) error
	Load() error

	Write() error
	Close() error
}

func New(
	baseDB *versiondb.Database,
	cfg *config.Config,
	ctx *snow.Context,
	localStake prometheus.Gauge,
	totalStake prometheus.Gauge,
	rewards reward.Calculator,
) State {
	metadata := metadata.NewState(baseDB)
	txState := transactions.NewState(baseDB, metadata, cfg, ctx,
		localStake, totalStake, rewards,
	)
	return &state{
		TxState:   txState,
		DataState: metadata,
	}
}

func NewMetered(
	baseDB *versiondb.Database,
	metrics prometheus.Registerer,
	cfg *config.Config,
	ctx *snow.Context,
	localStake prometheus.Gauge,
	totalStake prometheus.Gauge,
	rewards reward.Calculator,
) (State, error) {
	metadata := metadata.NewState(baseDB)
	txState, err := transactions.NewMeteredTransactionsState(
		baseDB, metadata, metrics, cfg, ctx,
		localStake, totalStake, rewards)
	return &state{
		TxState:   txState,
		DataState: metadata,
	}, err
}

type state struct {
	transactions.TxState
	metadata.DataState
}

func (s *state) SyncGenesis(
	genesisBlkID ids.ID,
	genesisTimestamp uint64,
	genesisInitialSupply uint64,
	genesisUtxos []*avax.UTXO,
	genesisValidator []*signed.Tx,
	genesisChains []*signed.Tx,
) error {
	err := s.DataState.SyncGenesis(genesisBlkID, genesisTimestamp, genesisInitialSupply)
	if err != nil {
		return err
	}

	return s.TxState.SyncGenesis(genesisUtxos, genesisValidator, genesisChains)
}

func (s *state) Load() error {
	// TxState depends on Metadata, hence we load metadata first
	err := s.LoadMetadata()
	if err != nil {
		return err
	}

	return s.LoadTxs()
}

func (s *state) Write() error {
	err := s.WriteMetadata()
	if err != nil {
		return err
	}

	return s.WriteTxs()
}

func (s *state) Close() error {
	// TxState depends on Metadata, hence we close metadata last
	err := s.CloseTxs()
	if err != nil {
		return err
	}

	return s.CloseMetadata()
}
