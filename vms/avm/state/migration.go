// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"iter"
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
	"github.com/ava-labs/avalanchego/codec"
)


var (
	_ ChainDB     = (*fooChainDB)(nil)
	_ avax.UTXODB = (*fooUTXODB)(nil)
	_ Migration   = (*NoMigration)(nil)
	_ Migration   = (*fooStateMigration)(nil)

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
		prevState State,
		baseDB *versiondb.Database,
		chainDataDir string,
	) (State, error)
}

type NoMigration struct{}

func (NoMigration) Migrate(
	_ logging.Logger,
	_ block.Parser,
	_ prometheus.Registerer,
	prevState State,
	_ *versiondb.Database,
	_ string,
) (State, error) {
	return prevState, nil
}

type fooStateMigration struct {
	CommitFrequency int

	updates int
}

func (f *fooStateMigration) Migrate(
	log logging.Logger,
	parser block.Parser,
	metrics prometheus.Registerer,
	baseDB *versiondb.Database,
	utxoDB database.Database,
	utxoIndexDB database.Database,
	txDB database.Database,
	blockIDDB database.Database,
	blockDB database.Database,
	singletonDB database.Database,
	chainDataDir string,
) (State, error) {
	next, err := newFooState(parser, metrics, chainDataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state: %w", err)
	}

	done, err := next.GetMigrationStatus()
	if errors.Is(err, database.ErrNotFound) {
		if err := next.PutMigrationStatus(false); err != nil {
			return nil, fmt.Errorf("failed to put migration status: %w", err)
		}

		log.Info("starting state migration")
	} else if err != nil {
		return nil, fmt.Errorf("failed to get state migration status: %w", err)
	}

	if done {
		log.Debug("skipping state migration")
		return next, nil
	}

	log.Info("resuming state migration")
	lastMigratedUTXO, _, err := next.GetLastMigratedUTXO()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated utxo: %w", err)
	}

	for itr := utxoDB.NewIteratorWithStart(lastMigratedUTXO[:]); itr.Next(); {
		utxo := &avax.UTXO{}
		if _, err := parser.Codec().Unmarshal(itr.Value(), utxo); err != nil {
			return nil, fmt.Errorf("failed to unmarshal utxo: %w", err)
		}

		next.AddUTXO(utxo)

		if err := utxoDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("failed to delete migrated utxo: %w", err)
		}

		if err := next.PutLastMigratedUTXO(utxo.InputID()); err != nil {
			return nil, fmt.Errorf("failed to put last migrated utxo: %w", err)
		}

		// TODO bugfix, we have different databases now
		ok, err := f.commit(next)
		if err != nil {
			return nil, fmt.Errorf("failed to commit db: %w", err)
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
		return nil, fmt.Errorf("failed to get last migrated tx: %w", err)
	}

	if ok {
		startingTxKey = lastMigratedTx[:]
	}

	for itr := txDB.NewIteratorWithStart(startingTxKey); itr.Next(); {
		if err := next.txDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate tx: %w", err)
		}

		if err := txDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("failed to delete migrated tx: %w", err)
		}

		txID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("failed to parse tx id: %w", err)
		}

		if err := next.PutLastMigratedTx(txID); err != nil {
			return nil, fmt.Errorf("failed to put last migrated tx: %w", err)
		}

		ok, err := f.commit(baseDB)
		if err != nil {
			return nil, fmt.Errorf("failed to commit db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Stringer("txID", txID))
	}

	log.Debug("migrating blocks")

	var startingBlkKey []byte
	lastMigratedBlk, ok, err := next.GetLastMigratedBlock()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated block: %w", err)
	}

	if ok {
		startingBlkKey = lastMigratedBlk[:]
	}

	// TODO check all iterator error values

	for itr := blockDB.NewIteratorWithStart(startingBlkKey); itr.Next(); {
		if err := next.blockDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block: %w", err)
		}

		if err := blockDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("failed to delete migrated block: %w", err)
		}

		blkID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("failed to parse block id: %w", err)
		}

		if err := next.PutLastMigratedBlock(blkID); err != nil {
			return nil, fmt.Errorf("failed to put last migrated block: %w", err)
		}

		ok, err := f.commit(baseDB)
		if err != nil {
			return nil, fmt.Errorf("failed to commit db: %w", err)
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
		return nil, fmt.Errorf("failed to get last migrated block height: %w", err)
	}

	if ok {
		startingBlkHeightKey = database.PackUInt64(lastMigratedBlkHeight)
	}

	for itr := blockIDDB.NewIteratorWithStart(startingBlkHeightKey); itr.Next(); {
		if err := blockIDDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block height: %w", err)
		}

		if err := blockIDDB.Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("failed to delete migrated block height: %w", err)
		}

		height, err := database.ParseUInt64(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("failed to parse block height: %w", err)
		}

		if err := next.PutLastMigratedBlockHeight(height); err != nil {
			return nil, fmt.Errorf("failed to put last migrated block height: %w", err)
		}

		ok, err := f.commit(baseDB)
		if err != nil {
			return nil, fmt.Errorf("failed to commit db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Uint64("height", height))
	}

	log.Debug("migrating singletons")
	next.state.SetLastAccepted(prev.GetLastAccepted())
	next.state.SetTimestamp(prev.GetTimestamp())

	ok, err = next.state.IsInitialized()
	if err != nil {
		return nil, fmt.Errorf("failed to check state initialization: %w", err)
	}
	if ok {
		if err := next.state.SetInitialized(); err != nil {
			return nil, fmt.Errorf("failed to set initialized: %w", err)
		}
	}

	if err := next.PutMigrationStatus(true); err != nil {
		return nil, fmt.Errorf("failed to put migration status: %w", err)
	}

	if err := next.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit state: %w", err)
	}

	log.Info("migration complete")
	return next.state, nil

	// TODO test coverage on atomic shit
}

// TODO commit firewood
// TODO test coverage on utxo migration
func (f *fooStateMigration) commit(db versiondb.Commitable) (bool, error) {
	f.updates++
	if f.updates%f.CommitFrequency != 0 {
		return false, nil
	}

	if err := db.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

func newFooState(
	parser block.Parser,
	metrics prometheus.Registerer,
	chainDataDir string,
) (fooState, error) {
	// The state db is used for state agreed upon by consensus
	stateDB, err := firewooddb.New(filepath.Join(chainDataDir, "state"))
	if err != nil {
		return fooState{}, fmt.Errorf("failed to initialize state db: %w", err)
	}

	// The local db is used for all other data not agreed upon by consensus
	localDB, err := pebbledb.New(
		filepath.Join(chainDataDir, "local"),
		nil,
		logging.NoLog{},
		metrics,
	)
	if err != nil {
		return fooState{}, fmt.Errorf("failed to initialize local db: %w", err)
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

	s, err := NewWithFormat[*fooChainDB](
		"foo_state",
		&fooChainDB{db: stateDB},
		localDB,
		versionDB,
		utxoIndexDB,
		txDB,
		blockIDDB,
		blockDB,
		singletonDB,
		func(db *fooChainDB) avax.UTXODB { return &FooUTXODB{stateDB: stateDB} },
		parser,
		metrics,
		false,
	)
	if err != nil {
		return fooState{}, fmt.Errorf("failed to initialize state: %w",
			err)
	}

	return fooState{
		state:       s,
		chainDB: stateDB,
		localDB:     localDB,
		versionDB:   versionDB,
		utxoIndexDB: utxoIndexDB,
		txDB:        txDB,
		blockIDDB:   blockIDDB,
		blockDB:     blockDB,
		singletonDB: singletonDB,
		migrationDB: migrationDB,
	}, nil
}


// TODO remove once the G fork is released
type fooState struct {
	state State
	// Chain state agreed upon by consensus
	chainDB *firewooddb.DB
	// Local indices that are not agreed-upon by consensus
	localDB     database.Database
	versionDB   *versiondb.Database
	utxoIndexDB database.Database
	txDB        database.Database
	blockIDDB   database.Database
	blockDB     database.Database
	singletonDB database.Database
	// Metadata for migrated keys
	// TODO delete in v1.15.x
	migrationDB database.Database
}

func (f fooState) GetMigrationStatus() (bool, error) {
	return database.GetBool(f.migrationDB, migrationStatusKey)
}

func (f fooState) PutMigrationStatus(done bool) error {
	return database.PutBool(f.migrationDB, migrationStatusKey, done)
}

func (f fooState) GetLastMigratedUTXO() (ids.ID, bool, error) {
	utxoIDBytes, err := f.migrationDB.Get(lastMigratedUTXOKey)
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

func (f fooState) PutLastMigratedUTXO(utxoID ids.ID) error {
	return f.migrationDB.Put(lastMigratedUTXOKey, utxoID[:])
}

func (f fooState) GetLastMigratedTx() (ids.ID, bool, error) {
	txIDBytes, err := f.migrationDB.Get(lastMigratedTxKey)
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

func (f fooState) PutLastMigratedTx(txID ids.ID) error {
	if err := f.migrationDB.Put(lastMigratedTxKey, txID[:]); err != nil {
		return err
	}

	return nil
}

func (f fooState) GetLastMigratedBlock() (ids.ID, bool, error) {
	blkIDBytes, err := f.migrationDB.Get(lastMigratedBlockKey)
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

func (f fooState) PutLastMigratedBlock(blkID ids.ID) error {
	return f.migrationDB.Put(lastMigratedBlockKey, blkID[:])
}

func (f fooState) GetLastMigratedBlockHeight() (uint64, bool, error) {
	height, err := database.GetUInt64(f.migrationDB, lastMigratedBlockHeightKey)
	if errors.Is(err, database.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return height, true, nil
}

func (f fooState) PutLastMigratedBlockHeight(height uint64) error {
	return f.migrationDB.Put(lastMigratedBlockHeightKey,
		database.PackUInt64(height))
}

type fooChainDB struct {
	db *firewooddb.DB
}

func (f fooChainDB) Close() error {
	return f.db.Close()
}

func (f fooChainDB) NewBatch() (database.Batch, error) {
	return f.db.NewBatch(), nil
}

func (f fooChainDB) Abort() {}

type fooUTXODB struct {
	db *firewooddb.DB
}

func (f fooUTXODB) Get(key []byte) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (f fooUTXODB) Put(key []byte, value []byte) error {
	//TODO implement me
	panic("implement me")
}

func (f fooUTXODB) Delete(key []byte) error {
	//TODO implement me
	panic("implement me")
}

func (f fooUTXODB) UTXOs(
	startingUTXOID ids.ID,
	codec codec.Manager,
) iter.Seq2[*avax.UTXO, error] {
	//TODO implement me
	panic("implement me")
}

func (f fooUTXODB) InitChecksum() error {
	return nil
}

func (f fooUTXODB) UpdateChecksum(ids.ID) {}

func (f fooUTXODB) Checksum() (ids.ID, error) {
	return f.db.Root()
}

func (f fooUTXODB) Close() error {
	return f.db.Close()
}
