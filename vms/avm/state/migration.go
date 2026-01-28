// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/firewood"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	migrationStatusKey   = []byte("status")
	lastMigratedUTXOKey  = []byte("utxo")
	lastMigratedTxKey    = []byte("tx")
	lastMigratedBlockKey = []byte("block")
)

type Migration interface {
	Migrate(
		log logging.Logger,
		parser block.Parser,
		metrics prometheus.Registerer,
		prev State,
		prevVersionDB *versiondb.Database,
		prevUTXODB database.Database,
		prevTXDB database.Database,
		prevBlockIDDB database.Database,
		prevBlockDB database.Database,
		prevSingletonDB database.Database,
		chainDataDir string,
		stopVertexID ids.ID,
		genesisTimestamp time.Time,
	) (State, ChainDB, error)
}

type NoMigration struct{}

var _ Migration = (*NoMigration)(nil)

func (NoMigration) Migrate(
	_ logging.Logger,
	_ block.Parser,
	_ prometheus.Registerer,
	prev State,
	vdb *versiondb.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ string,
	_ ids.ID,
	_ time.Time,
) (State, ChainDB, error) {
	return prev, &NoChainDB{VersionDB: vdb}, nil
}

type FirewoodMigration struct {
	CommitFrequency int

	updates int
}

var _ Migration = (*FirewoodMigration)(nil)

func (f *FirewoodMigration) Migrate(
	log logging.Logger,
	parser block.Parser,
	metrics prometheus.Registerer,
	prev State,
	prevVersionDB *versiondb.Database,
	prevUTXODB database.Database,
	prevTxDB database.Database,
	prevBlockIDDB database.Database,
	prevBlockDB database.Database,
	prevSingletonDB database.Database,
	chainDataDir string,
	stopVertexID ids.ID,
	genesisTimestamp time.Time,
) (State, ChainDB, error) {
	lastAccepted, err := prev.GetBlock(prev.GetLastAccepted())
	if err != nil {
		return nil, nil, fmt.Errorf("getting last accepted block: %w", err)
	}

	next, chainDB, err := newMigrationState(
		parser,
		metrics,
		chainDataDir,
		lastAccepted.Height(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("initializing state: %w", err)
	}

	done, err := next.GetMigrationStatus()
	if errors.Is(err, database.ErrNotFound) {
		if err := next.PutMigrationStatus(false); err != nil {
			return nil, nil, fmt.Errorf("putting migration status: %w", err)
		}
	} else if err != nil {
		return nil, nil, fmt.Errorf("getting state migration status: %w", err)
	}

	if done {
		log.Debug("skipping state migration")
		return next, chainDB, nil
	}

	log.Info("starting state migration")
	lastMigratedUTXO, err := next.GetLastMigratedUTXO()
	if err != nil {
		return nil, nil, fmt.Errorf("getting last migrated utxo: %w", err)
	}

	log.Debug("deleting utxos")
	itr := prevUTXODB.NewIteratorWithStart(lastMigratedUTXO[:])
	for itr.Next() {
		utxoID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, nil, fmt.Errorf("parsing utxo id: %w", err)
		}

		if err := next.PutLastMigratedUTXO(utxoID); err != nil {
			return nil, nil, fmt.Errorf("putting last migrated utxo: %w", err)
		}

		if err := prevUTXODB.Delete(itr.Key()); err != nil {
			return nil, nil, fmt.Errorf("deleting utxo: %w", err)
		}

		ok, err := f.commit(prevVersionDB, next.versionDB)
		if err != nil {
			return nil, nil, fmt.Errorf("committing db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress",
			zap.Stringer("utxoID", utxoID),
		)
	}

	if err := itr.Error(); err != nil {
		return nil, nil, fmt.Errorf("iterating db: %w", err)
	}

	log.Debug("deleting txs")
	lastMigratedTx, err := next.GetLastMigratedTx()
	if err != nil {
		return nil, nil, fmt.Errorf("getting last migrated tx: %w", err)
	}

	itr = prevTxDB.NewIteratorWithStart(lastMigratedTx[:])
	for itr.Next() {
		txID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, nil, fmt.Errorf("parsing tx id: %w", err)
		}

		if err := next.PutLastMigratedTx(txID); err != nil {
			return nil, nil, fmt.Errorf("putting last migrated tx: %w", err)
		}

		if err := prevTxDB.Delete(itr.Key()); err != nil {
			return nil, nil, fmt.Errorf("deleting migrated tx: %w", err)
		}

		ok, err := f.commit(prevVersionDB, next.versionDB)
		if err != nil {
			return nil, nil, fmt.Errorf("committing db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Stringer("txID", txID))
	}

	if err := itr.Error(); err != nil {
		return nil, nil, fmt.Errorf("iterating db: %w", err)
	}

	log.Debug("migrating blocks")
	lastMigratedBlk, err := next.GetLastMigratedBlock()
	if err != nil {
		return nil, nil, fmt.Errorf("getting last migrated block: %w", err)
	}

	itr = prevBlockDB.NewIteratorWithStart(lastMigratedBlk[:])
	for itr.Next() {
		blkID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, nil, fmt.Errorf("parsing block id: %w", err)
		}

		blk, err := parser.ParseBlock(itr.Value())
		if err != nil {
			return nil, nil, fmt.Errorf("parsing block: %w", err)
		}

		next.AddBlock(blk)

		if err := next.PutLastMigratedBlock(blkID); err != nil {
			return nil, nil, fmt.Errorf("putting last migrated block: %w", err)
		}

		if err := prevBlockDB.Delete(itr.Key()); err != nil {
			return nil, nil, fmt.Errorf("deleting migrated block: %w", err)
		}

		if err := prevBlockIDDB.Delete(database.PackUInt64(blk.Height())); err != nil {
			return nil, nil, fmt.Errorf("deleting migrated block: %w", err)
		}

		ok, err := f.commit(prevVersionDB, next.versionDB)
		if err != nil {
			return nil, nil, fmt.Errorf("committing db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Stringer("blkID", blkID))
	}

	if err := itr.Error(); err != nil {
		return nil, nil, fmt.Errorf("iterating db: %w", err)
	}

	log.Debug("migrating singletons")
	itr = prevSingletonDB.NewIterator()
	for itr.Next() {
		if err := next.singletonDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, nil, fmt.Errorf("migrating singleton: %w", err)
		}

		if err := prevSingletonDB.Delete(itr.Key()); err != nil {
			return nil, nil, fmt.Errorf("deleting singleton: %w", err)
		}
	}

	if err := itr.Error(); err != nil {
		return nil, nil, fmt.Errorf("iterating db: %w", err)
	}

	if err := next.PutMigrationStatus(true); err != nil {
		return nil, nil, fmt.Errorf("putting migration status: %w", err)
	}

	if err := next.versionDB.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing state: %w", err)
	}

	if err := prevVersionDB.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing state: %w", err)
	}

	// It is safe to call InitializeChainState because it was called before the
	// migration when creating the previous state - so it is not possible for us
	// to create two revisions at genesis.
	if err := next.InitializeChainState(stopVertexID, genesisTimestamp); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize chain state: %w", err)
	}

	log.Info("migration complete")

	return next, chainDB, nil
}

func (f *FirewoodMigration) commit(
	prev *versiondb.Database,
	next *versiondb.Database,
) (
	bool,
	error,
) {
	f.updates++
	if f.updates%f.CommitFrequency != 0 {
		return false, nil
	}

	if err := next.Commit(); err != nil {
		return false, err
	}

	if err := prev.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

type migrationState struct {
	State
	versionDB   *versiondb.Database
	singletonDB database.Database
	migrationDB database.Database
}

func newMigrationState(
	parser block.Parser,
	metrics prometheus.Registerer,
	chainDataDir string,
	lastAcceptedHeight uint64,
) (*migrationState, ChainDB, error) {
	// The state db is used for state agreed upon by consensus
	stateDB, err := firewood.New(filepath.Join(chainDataDir, "state"), lastAcceptedHeight)
	if err != nil {
		return nil, nil, fmt.Errorf("initializing state db: %w", err)
	}

	// The local db is used for all other data not agreed upon by consensus
	localDB, err := pebbledb.New(
		filepath.Join(chainDataDir, "local"),
		nil,
		logging.NoLog{},
		metrics,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("initializing local db: %w", err)
	}

	versionDB := versiondb.New(localDB)

	// Reserve a prefix for future re-indexing
	v1PrefixDB := prefixdb.New([]byte("v1"), versionDB)
	utxoIndexDB := prefixdb.New([]byte("utxo_index"), v1PrefixDB)
	txDB := prefixdb.New([]byte("tx"), v1PrefixDB)
	blockIDDB := prefixdb.New([]byte("block_id"), v1PrefixDB)
	blockDB := prefixdb.New([]byte("block"), v1PrefixDB)
	singletonDB := prefixdb.New([]byte("singleton"), v1PrefixDB)
	migrationDB := prefixdb.New([]byte("migration"), versionDB)
	chainDB := &firewoodDB{
		db:        stateDB,
		versionDB: versionDB,
	}

	s, err := NewWithFormat(
		"foo_state",
		chainDB,
		localDB,
		versionDB,
		utxoIndexDB,
		txDB,
		blockIDDB,
		blockDB,
		singletonDB,
		avax.NewFirewoodUTXODB(stateDB, parser.Codec()),
		parser,
		metrics,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("initializing state: %w", err)
	}

	return &migrationState{
			State:       s,
			versionDB:   versionDB,
			singletonDB: singletonDB,
			migrationDB: migrationDB,
		},
		chainDB,
		nil
}

func (m *migrationState) GetMigrationStatus() (bool, error) {
	return database.GetBool(m.migrationDB, migrationStatusKey)
}

func (m *migrationState) PutMigrationStatus(done bool) error {
	return database.PutBool(m.migrationDB, migrationStatusKey, done)
}

func (m *migrationState) GetLastMigratedUTXO() (ids.ID, error) {
	utxoIDBytes, err := m.migrationDB.Get(lastMigratedUTXOKey)
	if errors.Is(err, database.ErrNotFound) {
		return ids.ID{}, nil
	}
	if err != nil {
		return ids.ID{}, err
	}

	utxoID, err := ids.ToID(utxoIDBytes)
	if err != nil {
		return ids.ID{}, err
	}

	return utxoID, nil
}

func (m *migrationState) PutLastMigratedUTXO(utxoID ids.ID) error {
	return m.migrationDB.Put(lastMigratedUTXOKey, utxoID[:])
}

func (m *migrationState) GetLastMigratedTx() (ids.ID, error) {
	txIDBytes, err := m.migrationDB.Get(lastMigratedTxKey)
	if errors.Is(err, database.ErrNotFound) {
		return ids.ID{}, nil
	}
	if err != nil {
		return ids.ID{}, err
	}

	txID, err := ids.ToID(txIDBytes)
	if err != nil {
		return ids.ID{}, err
	}

	return txID, nil
}

func (m *migrationState) PutLastMigratedTx(txID ids.ID) error {
	if err := m.migrationDB.Put(lastMigratedTxKey, txID[:]); err != nil {
		return err
	}

	return nil
}

func (m *migrationState) GetLastMigratedBlock() (ids.ID, error) {
	blkIDBytes, err := m.migrationDB.Get(lastMigratedBlockKey)
	if errors.Is(err, database.ErrNotFound) {
		return ids.ID{}, nil
	}
	if err != nil {
		return ids.ID{}, err
	}

	blkID, err := ids.ToID(blkIDBytes)
	if err != nil {
		return ids.ID{}, err
	}

	return blkID, nil
}

func (m *migrationState) PutLastMigratedBlock(blkID ids.ID) error {
	return m.migrationDB.Put(lastMigratedBlockKey, blkID[:])
}
