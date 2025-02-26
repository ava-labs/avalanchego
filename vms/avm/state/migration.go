// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"path/filepath"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/merkledb/firewooddb"
)

var (
	_ ChainDB = (*FirewoodChainDB)(nil)
	_ Migration   = (*NoMigration)(nil)
	_ Migration = (*FooMigration)(nil)

	migrationStatusKey         = []byte("status")
	lastMigratedUTXOKey        = []byte("utxo")
	lastMigratedTxKey          = []byte("tx")
	lastMigratedBlockKey       = []byte("block")
	lastMigratedBlockHeightKey = []byte("block_height")
)

type Migration interface {
	Migrate(
		log logging.Logger,
		parser block.Parser,
		metrics prometheus.Registerer,
		prev State,
		prevBaseDB *versiondb.Database,
		prevUTXODB database.Database,
		prevTXDB database.Database,
		PrevBlockIDDB database.Database,
		PrevBlockDB database.Database,
		prevSingletonDB database.Database,
		chainDataDir string,
	) (State, error)
}

type NoMigration struct{}

func (NoMigration) Migrate(
	_ logging.Logger,
	_ block.Parser,
	_ prometheus.Registerer,
	prev State,
	_ *versiondb.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ string,
) (State, error) {
	return prev, nil
}

type FooMigration struct {
	CommitFrequency int

	updates int
}

// TODO delete state
// TODO log frequency?
func (f *FooMigration) Migrate(
	log logging.Logger,
	parser block.Parser,
	metrics prometheus.Registerer,
	_ State,
	baseDB *versiondb.Database,
	utxoDB database.Database,
	txDB database.Database,
	blockIDDB database.Database,
	blockDB database.Database,
	singletonDB database.Database,
	chainDataDir string,
) (State, error) {
	next, err := newMigrationState(parser, metrics, chainDataDir)
	if err != nil {
		return nil, fmt.Errorf("initializing state: %w", err)
	}

	done, err := next.GetMigrationStatus()
	if errors.Is(err, database.ErrNotFound) {
		if err := next.PutMigrationStatus(false); err != nil {
			return nil, fmt.Errorf("putting migration status: %w", err)
		}

		log.Info("starting state migration")
	} else if err != nil {
		return nil, fmt.Errorf("getting state migration status: %w", err)
	}

	if done {
		log.Debug("skipping state migration")
		return next, nil
	}

	log.Info("resuming state migration")
	lastMigratedUTXO, _, err := next.GetLastMigratedUTXO()
	if err != nil {
		return nil, fmt.Errorf("getting last migrated utxo: %w", err)
	}

	for itr := utxoDB.NewIteratorWithStart(lastMigratedUTXO[:]); itr.Next(); {
		utxo := &avax.UTXO{}
		if _, err := parser.Codec().Unmarshal(itr.Value(), utxo); err != nil {
			return nil, fmt.Errorf("unmarshaling utxo: %w", err)
		}

		next.AddUTXO(utxo)

		if err := utxoDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("deleting utxo: %w", err)
		}

		if err := next.PutLastMigratedUTXO(utxo.InputID()); err != nil {
			return nil, fmt.Errorf("putting last migrated utxo: %w", err)
		}

		// TODO bugfix, we have different databases now
		ok, err := f.commit(baseDB, next)
		if err != nil {
			return nil, fmt.Errorf("committing db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress",
			zap.Stringer("utxoID", utxo.InputID()),
		)
	}

	log.Debug("migrating txs")
	var startingTxKey []byte
	lastMigratedTx, ok, err := next.GetLastMigratedTx()
	if err != nil {
		return nil, fmt.Errorf("getting last migrated tx: %w", err)
	}

	if ok {
		startingTxKey = lastMigratedTx[:]
	}

	for itr := txDB.NewIteratorWithStart(startingTxKey); itr.Next(); {
		tx, err := parser.ParseTx(itr.Value())
		if err != nil {
			return nil, fmt.Errorf("parsing tx: %w", err)
		}

		next.State.AddTx(tx)

		if err := txDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("deleting migrated tx: %w", err)
		}

		if err := next.PutLastMigratedTx(tx.ID()); err != nil {
			return nil, fmt.Errorf("putting last migrated tx: %w", err)
		}

		ok, err := f.commit(baseDB, next)
		if err != nil {
			return nil, fmt.Errorf("committing db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Stringer("txID", tx.ID()))
	}

	log.Debug("migrating blocks")
	var startingBlkKey []byte
	lastMigratedBlk, ok, err := next.GetLastMigratedBlock()
	if err != nil {
		return nil, fmt.Errorf("getting last migrated block: %w", err)
	}

	if ok {
		startingBlkKey = lastMigratedBlk[:]
	}

	// TODO check all iterator error values
	for itr := blockDB.NewIteratorWithStart(startingBlkKey); itr.Next(); {
		blkID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("parsing block id: %w", err)
		}

		blk, err := parser.ParseBlock(itr.Value())
		if err != nil {
			return nil, fmt.Errorf("parsing block: %w", err)
		}

		next.AddBlock(blk)

		if err := blockDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("deleting migrated block: %w", err)
		}

		if err := next.PutLastMigratedBlock(blkID); err != nil {
			return nil, fmt.Errorf("putting last migrated block: %w", err)
		}

		ok, err := f.commit(baseDB, next)
		if err != nil {
			return nil, fmt.Errorf("committing db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Stringer("blkID", blkID))
	}

	log.Debug("migrating height index")

	var startingBlkHeightKey []byte
	lastMigratedBlkHeight, ok, err := next.GetLastMigratedBlockHeight()
	if err != nil {
		return nil, fmt.Errorf("getting last migrated block height: %w", err)
	}

	if ok {
		startingBlkHeightKey = database.PackUInt64(lastMigratedBlkHeight)
	}

	for itr := blockIDDB.NewIteratorWithStart(startingBlkHeightKey); itr.Next(); {
		if err := blockIDDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("migrating block height: %w", err)
		}

		if err := blockIDDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("deleting migrated block height: %w", err)
		}

		height, err := database.ParseUInt64(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("parsing block height: %w", err)
		}

		if err := next.PutLastMigratedBlockHeight(height); err != nil {
			return nil, fmt.Errorf("putting last migrated block height: %w",
				err)
		}

		ok, err := f.commit(baseDB, next)
		if err != nil {
			return nil, fmt.Errorf("committing db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Uint64("height", height))
	}

	log.Debug("migrating singletons")
	for itr := singletonDB.NewIterator(); itr.Next(); {
		if err := next.singletonDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("migrating singleton: %w", err)
		}
	}

	if err := next.PutMigrationStatus(true); err != nil {
		return nil, fmt.Errorf("putting migration status: %w", err)
	}

	if err := next.Commit(); err != nil {
		return nil, fmt.Errorf("committing state: %w", err)
	}

	log.Info("migration complete")
	return next, nil

	// TODO test coverage on atomic shit
}

// TODO commit firewood
// TODO test coverage on utxo migration
func (f *FooMigration) commit(
	prev *versiondb.Database,
	next *migrationState,
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

func newMigrationState(
	parser block.Parser,
	metrics prometheus.Registerer,
	chainDataDir string,
) (*migrationState, error) {
	// The state db is used for state agreed upon by consensus
	stateDB, err := firewooddb.New(filepath.Join(chainDataDir, "state"))
	if err != nil {
		return nil, fmt.Errorf("initializing state db: %w", err)
	}

	// The local db is used for all other data not agreed upon by consensus
	localDB, err := pebbledb.New(
		filepath.Join(chainDataDir, "local"),
		nil,
		logging.NoLog{},
		metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing local db: %w", err)
	}

	versionDB := versiondb.New(localDB)

	// Reserve a prefix for future re-indexing
	v1PrefixDB := prefixdb.New([]byte("v1"), versionDB)
	utxoIndexDB := prefixdb.New([]byte("utxo_index"), v1PrefixDB)
	txDB := prefixdb.New([]byte("tx"), v1PrefixDB)
	blockIDDB := prefixdb.New([]byte("block_id"), v1PrefixDB)
	blockDB := prefixdb.New([]byte("block"), v1PrefixDB)
	singletonDB := prefixdb.New([]byte("singleton"), v1PrefixDB)
	migrationDB := prefixdb.New([]byte("migration"), v1PrefixDB)

	s, err := NewWithFormat(
		"foo_state",
		&FirewoodChainDB{db: stateDB},
		localDB,
		utxoIndexDB,
		txDB,
		blockIDDB,
		blockDB,
		singletonDB,
		migrationDB,
		&FirewoodUTXODB{db: stateDB},
		parser,
		metrics,
		false)
	if err != nil {
		return nil, fmt.Errorf("initializing state: %w", err)
	}

	return &migrationState{
		State:       s,
		versionDB: versionDB,
		blockDB:     blockDB,
		txDB:        txDB,
		singletonDB: singletonDB,
		migrationDB: migrationDB,
	}, nil
}

type migrationState struct {
	State

	versionDB *versiondb.Database
	blockDB     database.Database
	txDB        database.Database
	singletonDB database.Database
	migrationDB database.Database
}

func (m *migrationState) GetMigrationStatus() (bool, error) {
	return database.GetBool(m.migrationDB, migrationStatusKey)
}

func (m *migrationState) PutMigrationStatus(done bool) error {
	return database.PutBool(m.migrationDB, migrationStatusKey, done)
}

func (m *migrationState) GetLastMigratedUTXO() (ids.ID, bool, error) {
	utxoIDBytes, err := m.migrationDB.Get(lastMigratedUTXOKey)
	if errors.Is(err, database.ErrNotFound) {
		return ids.ID{}, false, nil
	}
	if err != nil {
		return ids.ID{}, false, err
	}

	utxoID, err := ids.ToID(utxoIDBytes)
	if err != nil {
		return ids.ID{}, false, err
	}

	return utxoID, true, nil
}

func (m *migrationState) PutLastMigratedUTXO(utxoID ids.ID) error {
	return m.migrationDB.Put(lastMigratedUTXOKey, utxoID[:])
}

func (m *migrationState) GetLastMigratedTx() (ids.ID, bool, error) {
	txIDBytes, err := m.migrationDB.Get(lastMigratedTxKey)
	if errors.Is(err, database.ErrNotFound) {
		return ids.ID{}, false, nil
	}
	if err != nil {
		return ids.ID{}, false, err
	}

	txID, err := ids.ToID(txIDBytes)
	if err != nil {
		return ids.ID{}, false, err
	}

	return txID, true, nil
}

func (m *migrationState) PutLastMigratedTx(txID ids.ID) error {
	if err := m.migrationDB.Put(lastMigratedTxKey, txID[:]); err != nil {
		return err
	}

	return nil
}

func (m *migrationState) GetLastMigratedBlock() (ids.ID, bool, error) {
	blkIDBytes, err := m.migrationDB.Get(lastMigratedBlockKey)
	if errors.Is(err, database.ErrNotFound) {
		return ids.ID{}, false, nil
	}
	if err != nil {
		return ids.ID{}, false, err
	}

	blkID, err := ids.ToID(blkIDBytes)
	if err != nil {
		return ids.ID{}, false, err
	}

	return blkID, true, nil
}

func (m *migrationState) PutLastMigratedBlock(blkID ids.ID) error {
	return m.migrationDB.Put(lastMigratedBlockKey, blkID[:])
}

func (m *migrationState) GetLastMigratedBlockHeight() (uint64, bool, error) {
	height, err := database.GetUInt64(m.migrationDB, lastMigratedBlockHeightKey)
	if errors.Is(err, database.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return height, true, nil
}

func (m *migrationState) PutLastMigratedBlockHeight(height uint64) error {
	return m.migrationDB.Put(
		lastMigratedBlockHeightKey,
		database.PackUInt64(height),
	)
}
