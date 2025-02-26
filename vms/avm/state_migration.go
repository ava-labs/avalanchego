// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"path/filepath"
	"github.com/ava-labs/avalanchego/database/pebbledb"
)

var (
	_ state.State = (*graniteState)(nil)
	_ StateMigration = (*NoMigration)(nil)
	_ StateMigration  = (*GraniteMigration)(nil)

	migrationStatusKey         = []byte("status")
	lastMigratedUTXOKey        = []byte("utxo")
	lastMigratedTxKey          = []byte("tx")
	lastMigratedBlockKey       = []byte("block")
	lastMigratedBlockHeightKey = []byte("block_height")
)

type StateMigration interface {
	Migrate(
		ctx context.Context,
		log logging.Logger,
		parser block.Parser,
		metrics prometheus.Registerer,
		prevState state.State,
		baseDB *versiondb.Database,
		chainDataDir string,
	) (state.State, error)
}

type NoMigration struct{}

func (NoMigration) Migrate(
	_ context.Context,
	_ logging.Logger,
	_ block.Parser,
	_ prometheus.Registerer,
	prevState state.State,
	_ *versiondb.Database,
	_ string,
) (state.State, error) {
	return prevState, nil
}

type GraniteMigration struct {
	CommitFrequency int

	updates int
}

func (g *GraniteMigration) Migrate(
	ctx context.Context,
	log logging.Logger,
	parser block.Parser,
	metrics prometheus.Registerer,
	prevState state.State,
	baseDB *versiondb.Database,
	chainDataDir string,
) (state.State, error) {
	next, err := newGraniteState(parser, metrics, chainDataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state: %w", err)
	}

	done, ok, err := next.GetMigrationStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get state migration status: %w", err)
	}

	if done {
		log.Debug("skipping state migration")
		return next, nil
	}

	if ok {
		log.Info("resuming state migration")
	} else {
		if err := next.PutMigrationStatus(false); err != nil {
			return nil, fmt.Errorf("failed to put migration status: %w", err)
		}

		log.Info("starting state migration")
	}

	lastMigratedUTXO, _, err := next.GetLastMigratedUTXO()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated utxo: %w", err)
	}

	for utxo, err := range prevState.UTXOs(lastMigratedUTXO) {
		if err != nil {
			return nil, fmt.Errorf("failed to get utxo: %w", err)
		}

		next.AddUTXO(utxo)
		utxoID := utxo.InputID()
		if err := prevState.UTXODB().Delete(utxoID[:]); err != nil {
			return nil, fmt.Errorf("failed to delete migrated utxo: %w", err)
		}

		if err := next.PutLastMigratedUTXO(utxo.InputID()); err != nil {
			return nil, fmt.Errorf("failed to put last migrated utxo: %w", err)
		}

		// TODO bugfix, we have different databases now
		ok, err := g.commit(next)
		if err != nil {
			return nil, fmt.Errorf("failed to commit db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Stringer("utxoID", utxoID))
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

	for itr := prevState.TXDB().NewIteratorWithStart(startingTxKey); itr.Next(); {
		if err := next.txDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate tx: %w", err)
		}

		if err := prevState.TXDB().Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("failed to delete migrated tx: %w", err)
		}

		txID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("failed to parse tx id: %w", err)
		}

		if err := next.PutLastMigratedTx(txID); err != nil {
			return nil, fmt.Errorf("failed to put last migrated tx: %w", err)
		}

		ok, err := g.commit(baseDB)
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

	for itr := prevState.BlockDB().NewIteratorWithStart(startingBlkKey); itr.Next(); {
		if err := next.state.BlockDB().Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block: %w", err)
		}

		if err := prevState.BlockDB().Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("failed to delete migrated block: %w", err)
		}

		blkID, err := ids.ToID(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("failed to parse block id: %w", err)
		}

		if err := next.PutLastMigratedBlock(blkID); err != nil {
			return nil, fmt.Errorf("failed to put last migrated block: %w", err)
		}

		ok, err := g.commit(baseDB)
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

	for itr := prevState.BlockIDDB().NewIteratorWithStart(startingBlkHeightKey); itr.Next(); {
		if err := next.state.BlockIDDB().Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block height: %w", err)
		}

		if err := prevState.BlockIDDB().Delete(itr.Key()); err != nil {
			return nil, fmt.Errorf("failed to delete migrated block height: %w", err)
		}

		height, err := database.ParseUInt64(itr.Key())
		if err != nil {
			return nil, fmt.Errorf("failed to parse block height: %w", err)
		}

		if err := next.PutLastMigratedBlockHeight(height); err != nil {
			return nil, fmt.Errorf("failed to put last migrated block height: %w", err)
		}

		ok, err := g.commit(baseDB)
		if err != nil {
			return nil, fmt.Errorf("failed to commit db: %w", err)
		}
		if !ok {
			continue
		}

		log.Verbo("committed migration progress", zap.Uint64("height", height))
	}

	log.Debug("migrating singletons")
	next.state.SetLastAccepted(prevState.GetLastAccepted())
	next.state.SetTimestamp(prevState.GetTimestamp())

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
	return next, nil
}

// TODO test coverage on utxo migration
func (g *GraniteMigration) commit(db versiondb.Commitable) (bool, error) {
	g.updates++
	if g.updates%g.CommitFrequency != 0 {
		return false, nil
	}

	if err := db.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

func newGraniteState(
	parser block.Parser,
	metrics prometheus.Registerer,
	chainDataDir string,
) (graniteState, error) {
	// The state db is used for state agreed upon by consensus
	stateDB, err := firewood.New(filepath.Join(chainDataDir, "state"))
	if err != nil {
		return graniteState{}, fmt.Errorf("failed to initialize state db: %w", err)
	}

	// TODO need a root on this
	utxoDB := prefixdb.New([]byte("utxo"), stateDB)

	// The local db is used for all other data not agreed upon by consensus
	localDB, err := pebbledb.New(
		filepath.Join(chainDataDir, "local"),
		nil,
		logging.NoLog{},
		metrics,
	)
	if err != nil {
		return graniteState{}, fmt.Errorf("failed to initialize local db: %w", err)
	}

	// Reserve a prefix for future re-indexing
	v1PrefixDB := prefixdb.New([]byte("v1"), localDB)
	utxoIndexDB := prefixdb.New([]byte("utxo_index"), v1PrefixDB)
	txDB := prefixdb.New([]byte("tx"), v1PrefixDB)
	blockIDDB := prefixdb.New([]byte("block_id"), v1PrefixDB)
	blockDB := prefixdb.New([]byte("block"), v1PrefixDB)
	singletonDB := prefixdb.New([]byte("singleton"), v1PrefixDB)
	migrationDB := prefixdb.New([]byte("migration"), v1PrefixDB)

	s, err := state.NewWithFormat(
		"",
		baseDB,
		utxoMerkleDB,
		utxoIndexDB,
		txDB,
		blockIDDB,
		blockDB,
		singletonDB,
		parser,
		metrics,
		false, // Merkle roots are used instead of the checksum
	)
	if err != nil {
		return graniteState{}, fmt.Errorf("failed to initialize state: %w",
			err)
	}

	return graniteState{
		state:         s,
		stateDB: v1PrefixDB,
		stateMerkleDB: stateMerkleDB,
		localDB: localDB,
		utxoDB:        utxoDB,
		utxoMerkleDB:  utxoMerkleDB,
		utxoIndexDB:   utxoIndexDB,
		txDB:          txDB,
		blockIDDB:     blockIDDB,
		blockDB:       blockDB,
		singletonDB:   singletonDB,
		migrationDB:   migrationDB,
	}, nil
}

// TODO remove once the G fork is released
// TODO figure out atomic writes and shit since we're not using vdb
type graniteState struct {
	state   state.State
	baseDB        *versiondb.Database
	stateDB       database.Database
	stateMerkleDB merkledb.MerkleDB
	localDB database.Database
	utxoDB        database.Database
	utxoMerkleDB  merkledb.MerkleDB
	utxoIndexDB   database.Database
	txDB          database.Database
	blockIDDB     database.Database
	blockDB       database.Database
	singletonDB   database.Database
	// Metadata for migrated keys
	// TODO delete in v1.15.x
	migrationDB database.Database
}

func (g graniteState) GetMigrationStatus() (bool, bool, error) {
	done, err := database.GetBool(g.migrationDB, migrationStatusKey)
	if errors.Is(err, database.ErrNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}

	return done, true, nil
}

func (g graniteState) PutMigrationStatus(done bool) error {
	return database.PutBool(g.migrationDB, migrationStatusKey, done)
}

func (g graniteState) GetLastMigratedUTXO() (ids.ID, bool, error) {
	utxoIDBytes, err := g.migrationDB.Get(lastMigratedUTXOKey)
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

func (g graniteState) PutLastMigratedUTXO(utxoID ids.ID) error {
	return g.migrationDB.Put(lastMigratedUTXOKey, utxoID[:])
}

func (g graniteState) GetLastMigratedTx() (ids.ID, bool, error) {
	txIDBytes, err := g.migrationDB.Get(lastMigratedTxKey)
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

func (g graniteState) PutLastMigratedTx(txID ids.ID) error {
	if err := g.migrationDB.Put(lastMigratedTxKey, txID[:]); err != nil {
		return err
	}

	return nil
}

func (g graniteState) GetLastMigratedBlock() (ids.ID, bool, error) {
	blkIDBytes, err := g.migrationDB.Get(lastMigratedBlockKey)
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

func (g graniteState) PutLastMigratedBlock(blkID ids.ID) error {
	return g.migrationDB.Put(lastMigratedBlockKey, blkID[:])
}

func (g graniteState) GetLastMigratedBlockHeight() (uint64, bool, error) {
	height, err := database.GetUInt64(g.migrationDB, lastMigratedBlockHeightKey)
	if errors.Is(err, database.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return height, true, nil
}

func (g graniteState) PutLastMigratedBlockHeight(height uint64) error {
	return g.migrationDB.Put(lastMigratedBlockHeightKey, database.PackUInt64(height))
}

func (g graniteState) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	return g.state.GetUTXO(utxoID)
}

func (g graniteState) GetTx(txID ids.ID) (*txs.Tx, error) {
	return g.state.GetTx(txID)
}

func (g graniteState) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	return g.state.GetBlockIDAtHeight(height)
}

func (g graniteState) GetBlock(blkID ids.ID) (block.Block, error) {
	return g.state.GetBlock(blkID)
}

func (g graniteState) GetLastAccepted() ids.ID {
	return g.state.GetLastAccepted()
}

func (g graniteState) GetTimestamp() time.Time {
	return g.state.GetTimestamp()
}

func (g graniteState) AddUTXO(utxo *avax.UTXO) {
	g.state.AddUTXO(utxo)
}

func (g graniteState) DeleteUTXO(utxoID ids.ID) {
	g.state.DeleteUTXO(utxoID)
}

func (g graniteState) AddTx(tx *txs.Tx) {
	g.state.AddTx(tx)
}

func (g graniteState) AddBlock(blk block.Block) {
	g.state.AddBlock(blk)
}

func (g graniteState) SetLastAccepted(blkID ids.ID) {
	g.state.SetLastAccepted(blkID)
}

func (g graniteState) SetTimestamp(t time.Time) {
	g.state.SetTimestamp(t)
}

func (g graniteState) UTXOIDs(
	addr []byte,
	previous ids.ID,
	limit int,
) ([]ids.ID, error) {
	return g.state.UTXOIDs(addr, previous, limit)
}

func (g graniteState) UTXOs(startingUTXOID ids.ID) iter.Seq2[*avax.UTXO, error] {
	return g.state.UTXOs(startingUTXOID)
}

func (g graniteState) IsInitialized() (bool, error) {
	return g.state.IsInitialized()
}

func (g graniteState) SetInitialized() error {
	return g.state.SetInitialized()
}

// InitializeChainState cannot be called because pre-G fork state is initialized
// prior to migration
func (g graniteState) InitializeChainState(
	stopVertexID ids.ID,
	genesisTimestamp time.Time,
) error {
	return g.state.InitializeChainState(stopVertexID, genesisTimestamp)
}

func (g graniteState) Abort() {
	g.state.Abort()
}

func (g graniteState) Commit() error {
	return g.state.Commit()
}

func (g graniteState) CommitBatch() (database.Batch, error) {
	return g.state.CommitBatch()
}

func (g graniteState) Checksum(ctx context.Context) (ids.ID, error) {
	stateRoot, err := g.stateMerkleDB.GetMerkleRoot(ctx)
	if err != nil {
		return ids.ID{}, fmt.Errorf("failed to get utxo root: %w", err)
	}

	return stateRoot, nil
}
func (g graniteState) UTXODB() database.Database {
	//TODO implement me
	panic("implement me")
}

func (g graniteState) TXDB() database.Database {
	//TODO implement me
	panic("implement me")
}

func (g graniteState) BlockIDDB() database.Database {
	//TODO implement me
	panic("implement me")
}

func (g graniteState) BlockDB() database.Database {
	//TODO implement me
	panic("implement me")
}

func (g graniteState) Close() error {
	return errors.Join(
		g.migrationDB.Close(),
		g.singletonDB.Close(),
		g.blockDB.Close(),
		g.blockIDDB.Close(),
		g.txDB.Close(),
		g.utxoIndexDB.Close(),
		g.utxoMerkleDB.Close(),
		g.utxoDB.Close(),
		g.localDB.Close(),
		g.stateMerkleDB.Close(),
		g.stateDB.Close(),
		g.baseDB.Close(),
	)
}
