// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/metadata"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	"github.com/prometheus/client_golang/prometheus"

	p_genesis "github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

var _ State = &state{}

// Mutable interface collects all methods updating
// metadata and transactions state upon blocks execution
type Mutable interface {
	transactions.Mutable
	metadata.Mutable
}

// Content interface collects all methods to query and mutate
// all metadata and transactions state. Note this Content
// is a superset of Mutable
type Content interface {
	blocks.Content
	transactions.Content
	metadata.Content
}

// State interface collects Content along with all methods
// used to initialize metadata and transactions state db
// upon vm initialization, along with methods to
// persist updated state.
type State interface {
	Content

	// Upon vm initialization, SyncGenesis loads
	// information from genesis block as marshalled from bytes
	Sync(genesisBytes []byte) error
	Load() error

	Write() error

	Abort()
	Commit() error
	CommitBatch() (database.Batch, error)
	Close() error
}

func New(
	db database.Database,
	cfg *config.Config,
	ctx *snow.Context,
	localStake prometheus.Gauge,
	totalStake prometheus.Gauge,
	rewards reward.Calculator,
	genesisBytes []byte,
) (State, error) {
	baseDB := versiondb.New(db)

	metadata := metadata.NewState(baseDB)
	txState := transactions.NewState(baseDB, metadata, cfg, ctx,
		localStake, totalStake, rewards,
	)
	blkState := blocks.NewState(baseDB)

	globalState := &state{
		baseDB:    baseDB,
		DataState: metadata,
		TxState:   txState,
		BlkState:  blkState,
	}

	// Finally create and load genesis block, once txs and blocks
	// creators and verifiers are instantiated
	if err := globalState.Sync(genesisBytes); err != nil {
		// Drop any errors on close to return the first error
		_ = globalState.Close()
		return globalState, err
	}

	return globalState, nil
}

func NewMetered(
	db database.Database,
	metrics prometheus.Registerer,
	cfg *config.Config,
	ctx *snow.Context,
	localStake prometheus.Gauge,
	totalStake prometheus.Gauge,
	rewards reward.Calculator,
	genesisBytes []byte,
) (State, error) {
	baseDB := versiondb.New(db)

	metadata := metadata.NewState(baseDB)
	txState, err := transactions.NewMeteredTransactionsState(
		baseDB, metadata, metrics, cfg, ctx,
		localStake, totalStake, rewards)
	if err != nil {
		// Drop any errors on close to return the first error
		_ = txState.CloseTxs()

		return nil, err
	}
	blkState, err := blocks.NewMeteredState(baseDB, metrics)
	if err != nil {
		// Drop any errors on close to return the first error
		_ = blkState.CloseBlocks()

		return nil, err
	}

	globalState := &state{
		baseDB:    baseDB,
		DataState: metadata,
		TxState:   txState,
		BlkState:  blkState,
	}

	// Finally create and load genesis block, once txs and blocks
	// creators and verifiers are instantiated
	if err := globalState.Sync(genesisBytes); err != nil {
		// Drop any errors on close to return the first error
		_ = globalState.Close()
		return globalState, err
	}

	return globalState, nil
}

type state struct {
	baseDB *versiondb.Database

	metadata.DataState
	transactions.TxState
	blocks.BlkState
}

func (s *state) Sync(genesisBytes []byte) error {
	shouldInit, err := s.ShouldInit()
	if err != nil {
		return fmt.Errorf(
			"failed to check if the database is initialized: %w",
			err,
		)
	}

	// If the database is empty, create the platform chain anew using the
	// provided genesis state
	if shouldInit {
		if err := s.syncGenesis(genesisBytes); err != nil {
			return fmt.Errorf("failed to initialize the database: %w", err)
		}

		if err := s.DoneInit(); err != nil {
			return fmt.Errorf("failed to initialize the database: %w", err)
		}

		if err := s.Commit(); err != nil {
			return fmt.Errorf("failed to initialize the database: %w", err)
		}
	}

	if err := s.Load(); err != nil {
		return fmt.Errorf(
			"failed to load the database state: %w",
			err,
		)
	}

	return nil
}

func (s *state) syncGenesis(genesisBytes []byte) error {
	genesisBlkID, err := s.BlkState.SyncGenesis(genesisBytes)
	if err != nil {
		return err
	}

	genesisState, err := p_genesis.Parse(genesisBytes)
	if err != nil {
		return err
	}

	if err := s.DataState.SyncGenesis(
		genesisBlkID,
		genesisState.Timestamp,
		genesisState.InitialSupply,
	); err != nil {
		return err
	}

	return s.TxState.SyncGenesis(
		genesisState.UTXOs,
		genesisState.Validators,
		genesisState.Chains,
	)
}

func (s *state) Load() error {
	// TxState depends on Metadata, hence we load metadata first
	if err := s.LoadMetadata(); err != nil {
		return err
	}

	return s.LoadTxs() // Nothing to load for blocks
}

func (s *state) Write() error {
	if err := s.WriteMetadata(); err != nil {
		return err
	}

	if err := s.WriteTxs(); err != nil {
		return err
	}

	return s.WriteBlocks()
}

func (s *state) Close() error {
	// TxState depends on Metadata, hence we close metadata last
	// Blocks first for simmetry
	errs := wrappers.Errs{}
	errs.Add(
		s.CloseBlocks(),
		s.CloseTxs(),
		s.CloseMetadata(),
		s.baseDB.Close(),
	)
	return errs.Err
}

func (s *state) Abort() { s.baseDB.Abort() }

func (s *state) CommitBatch() (database.Batch, error) {
	if err := s.Write(); err != nil {
		return nil, err
	}

	return s.baseDB.CommitBatch()
}

func (s *state) Commit() error {
	defer s.Abort()
	batch, err := s.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}
