// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
)

var (
	_ state.Interface       = (*gForkState)(nil)
	_ StateMigrationFactory = (*NoStateMigrationFactory)(nil)
	_ StateMigration        = (*NoStateMigration)(nil)
	_ StateMigrationFactory = (*GForkStateMigrationFactory)(nil)
	_ StateMigration        = (*GForkStateMigration)(nil)

	migrationStatusKey         = []byte("status")
	lastMigratedUTXOKey        = []byte("utxo")
	lastMigratedTxKey          = []byte("tx")
	lastMigratedBlockKey       = []byte("block")
	lastMigratedBlockHeightKey = []byte("block_height")
)

type StateMigrationFactory interface {
	New(
		log logging.Logger,
		parser block.Parser,
		metrics prometheus.Registerer,
	) StateMigration
}

type StateMigration interface {
	Migrate(
		ctx context.Context,
		prevState state.Interface,
		baseDB *versiondb.Database,
		prevUTXODB database.Database,
		prevTXDB database.Database,
		prevBlockIDDB database.Database,
		prevBlockDB database.Database,
	) (state.Interface, error)
}

type NoStateMigrationFactory struct{}

func (NoStateMigrationFactory) New(
	logging.Logger,
	block.Parser,
	prometheus.Registerer,
) StateMigration {
	return NoStateMigration{}
}

type NoStateMigration struct{}

func (NoStateMigration) Migrate(
	_ context.Context,
	prevState state.Interface,
	_ *versiondb.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
) (state.Interface, error) {
	return prevState, nil
}

type GForkStateMigrationFactory struct {
	CommitFrequency int
}

func (g GForkStateMigrationFactory) New(
	log logging.Logger,
	parser block.Parser,
	metrics prometheus.Registerer,
) StateMigration {
	return &GForkStateMigration{
		commitFrequency: g.CommitFrequency,
		log:             log,
		parser:          parser,
		metrics:         metrics,
	}
}

type GForkStateMigration struct {
	commitFrequency int
	log             logging.Logger
	parser          block.Parser
	metrics         prometheus.Registerer

	updates int
}

func (g *GForkStateMigration) Migrate(
	ctx context.Context,
	prevState state.Interface,
	baseDB *versiondb.Database,
	prevUTXODB database.Database,
	prevTXDB database.Database,
	prevBlockIDDB database.Database,
	prevBlockDB database.Database,
) (state.Interface, error) {
	next, err := newGForkState(ctx, baseDB, prefixdb.New([]byte("v1.14.x"), baseDB), g.parser, g.metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state: %w", err)
	}

	done, ok, err := next.GetMigrationStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get state migration status: %w", err)
	}

	if done {
		g.log.Debug("skipping state migration")
		return next, nil
	}

	if ok {
		g.log.Info("resuming state migration")
	} else {
		if err := next.PutMigrationStatus(false); err != nil {
			return nil, fmt.Errorf("failed to put migration status: %w", err)
		}

		g.log.Info("starting state migration")
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
		if err := prevUTXODB.Delete(utxoID[:]); err != nil {
			return nil, fmt.Errorf("failed to delete migrated utxo: %w", err)
		}

		if err := next.PutLastMigratedUTXO(utxo.InputID()); err != nil {
			return nil, fmt.Errorf("failed to put last migrated utxo: %w", err)
		}

		ok, err := g.commit(next)
		if err != nil {
			return nil, fmt.Errorf("failed to commit db: %w", err)
		}
		if !ok {
			continue
		}

		g.log.Verbo("committed migration progress", zap.Stringer("utxoID", utxoID))
	}

	g.log.Debug("migrating txs")
	var startingTxKey []byte
	lastMigratedTx, ok, err := next.GetLastMigratedTx()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated tx: %w", err)
	}

	if ok {
		startingTxKey = lastMigratedTx[:]
	}

	for itr := prevTXDB.NewIteratorWithStart(startingTxKey); itr.Next(); {
		if err := next.txDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate tx: %w", err)
		}

		if err := prevTXDB.Delete(itr.Key()); err != nil {
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

		g.log.Verbo("committed migration progress", zap.Stringer("txID", txID))
	}

	g.log.Debug("migrating blocks")

	var startingBlkKey []byte
	lastMigratedBlk, ok, err := next.GetLastMigratedBlock()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated block: %w", err)
	}

	if ok {
		startingBlkKey = lastMigratedBlk[:]
	}

	for itr := prevBlockDB.NewIteratorWithStart(startingBlkKey); itr.Next(); {
		if err := next.state.BlockDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block: %w", err)
		}

		if err := prevBlockDB.Delete(itr.Key()); err != nil {
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

		g.log.Verbo("committed migration progress", zap.Stringer("blkID", blkID))
	}

	g.log.Debug("migrating height index")

	var startingBlkHeightKey []byte
	lastMigratedBlkHeight, ok, err := next.GetLastMigratedBlockHeight()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated block height: %w", err)
	}

	if ok {
		startingBlkHeightKey = database.PackUInt64(lastMigratedBlkHeight)
	}

	for itr := prevBlockIDDB.NewIteratorWithStart(startingBlkHeightKey); itr.Next(); {
		if err := next.state.BlockIDDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block height: %w", err)
		}

		if err := prevBlockIDDB.Delete(itr.Key()); err != nil {
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

		g.log.Verbo("committed migration progress", zap.Uint64("height", height))
	}

	g.log.Debug("migrating singletons")
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

	g.log.Info("migration complete")
	return next, nil
}

// TODO test coverage on utxo migration
func (g *GForkStateMigration) commit(db versiondb.Commitable) (bool, error) {
	g.updates++
	if g.updates%g.commitFrequency != 0 {
		return false, nil
	}

	if err := db.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

func newGForkState(
	ctx context.Context,
	baseDB *versiondb.Database,
	db database.Database,
	parser block.Parser,
	metrics prometheus.Registerer,
) (gForkState, error) {
	merkleDBConfig := merkledb.NewConfig()

	stateMerkleDBConfig := merkleDBConfig
	stateMerkleDBConfig.Namespace = "state"

	// Data required for execution is stored under the state prefix
	stateDB := prefixdb.New([]byte("state"), db)
	stateMerkleDB, err := merkledb.New(
		ctx,
		stateDB,
		stateMerkleDBConfig,
	)
	if err != nil {
		return gForkState{}, fmt.Errorf("failed to initialize state db: %w", err)
	}

	utxoDB := prefixdb.New([]byte("utxo"), stateMerkleDB)
	utxoMerkleDBConfig := merkleDBConfig
	utxoMerkleDBConfig.Namespace = "utxo_state"
	utxoMerkleDB, err := merkledb.New(ctx, utxoDB, utxoMerkleDBConfig)
	if err != nil {
		return gForkState{}, fmt.Errorf("failed to initialize utxo db: %w", err)
	}

	// All other data is stored under the metadata prefix
	metadataDB := prefixdb.New([]byte("metadata"), db)
	utxoIndexDB := prefixdb.New([]byte("utxo_index"), metadataDB)
	txDB := prefixdb.New([]byte("tx"), metadataDB)
	blockIDDB := prefixdb.New([]byte("block_id"), metadataDB)
	blockDB := prefixdb.New([]byte("block"), metadataDB)
	singletonDB := prefixdb.New([]byte("singleton"), metadataDB)
	migrationDB := prefixdb.New([]byte("migration"), metadataDB)

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
		return gForkState{}, fmt.Errorf("failed to initialize state: %w", err)
	}

	return gForkState{
		state:         s,
		baseDB:        baseDB,
		stateDB:       stateDB,
		stateMerkleDB: stateMerkleDB,
		metadataDB:    metadataDB,
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
type gForkState struct {
	state         *state.State
	baseDB        *versiondb.Database
	stateDB       database.Database
	stateMerkleDB merkledb.MerkleDB
	metadataDB    database.Database
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

func (g gForkState) GetMigrationStatus() (bool, bool, error) {
	done, err := database.GetBool(g.migrationDB, migrationStatusKey)
	if errors.Is(err, database.ErrNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}

	return done, true, nil
}

func (g gForkState) PutMigrationStatus(done bool) error {
	return database.PutBool(g.migrationDB, migrationStatusKey, done)
}

func (g gForkState) GetLastMigratedUTXO() (ids.ID, bool, error) {
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

func (g gForkState) PutLastMigratedUTXO(utxoID ids.ID) error {
	return g.migrationDB.Put(lastMigratedUTXOKey, utxoID[:])
}

func (g gForkState) GetLastMigratedTx() (ids.ID, bool, error) {
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

func (g gForkState) PutLastMigratedTx(txID ids.ID) error {
	if err := g.migrationDB.Put(lastMigratedTxKey, txID[:]); err != nil {
		return err
	}

	return nil
}

func (g gForkState) GetLastMigratedBlock() (ids.ID, bool, error) {
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

func (g gForkState) PutLastMigratedBlock(blkID ids.ID) error {
	return g.migrationDB.Put(lastMigratedBlockKey, blkID[:])
}

func (g gForkState) GetLastMigratedBlockHeight() (uint64, bool, error) {
	height, err := database.GetUInt64(g.migrationDB, lastMigratedBlockHeightKey)
	if errors.Is(err, database.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return height, true, nil
}

func (g gForkState) PutLastMigratedBlockHeight(height uint64) error {
	return g.migrationDB.Put(lastMigratedBlockHeightKey, database.PackUInt64(height))
}

func (g gForkState) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	return g.state.GetUTXO(utxoID)
}

func (g gForkState) GetTx(txID ids.ID) (*txs.Tx, error) {
	return g.state.GetTx(txID)
}

func (g gForkState) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	return g.state.GetBlockIDAtHeight(height)
}

func (g gForkState) GetBlock(blkID ids.ID) (block.Block, error) {
	return g.state.GetBlock(blkID)
}

func (g gForkState) GetLastAccepted() ids.ID {
	return g.state.GetLastAccepted()
}

func (g gForkState) GetTimestamp() time.Time {
	return g.state.GetTimestamp()
}

func (g gForkState) AddUTXO(utxo *avax.UTXO) {
	g.state.AddUTXO(utxo)
}

func (g gForkState) DeleteUTXO(utxoID ids.ID) {
	g.state.DeleteUTXO(utxoID)
}

func (g gForkState) AddTx(tx *txs.Tx) {
	g.state.AddTx(tx)
}

func (g gForkState) AddBlock(blk block.Block) {
	g.state.AddBlock(blk)
}

func (g gForkState) SetLastAccepted(blkID ids.ID) {
	g.state.SetLastAccepted(blkID)
}

func (g gForkState) SetTimestamp(t time.Time) {
	g.state.SetTimestamp(t)
}

func (g gForkState) UTXOIDs(
	addr []byte,
	previous ids.ID,
	limit int,
) ([]ids.ID, error) {
	return g.state.UTXOIDs(addr, previous, limit)
}

func (g gForkState) UTXOs(startingUTXOID ids.ID) iter.Seq2[*avax.UTXO, error] {
	return g.state.UTXOs(startingUTXOID)
}

func (g gForkState) IsInitialized() (bool, error) {
	return g.state.IsInitialized()
}

func (g gForkState) SetInitialized() error {
	return g.state.SetInitialized()
}

// InitializeChainState cannot be called because pre-G fork state is initialized
// prior to migration
func (g gForkState) InitializeChainState(
	stopVertexID ids.ID,
	genesisTimestamp time.Time,
) error {
	return g.state.InitializeChainState(stopVertexID, genesisTimestamp)
}

func (g gForkState) Abort() {
	g.state.Abort()
}

func (g gForkState) Commit() error {
	return g.state.Commit()
}

func (g gForkState) CommitBatch() (database.Batch, error) {
	return g.state.CommitBatch()
}

func (g gForkState) Checksum(ctx context.Context) (ids.ID, error) {
	stateRoot, err := g.stateMerkleDB.GetMerkleRoot(ctx)
	if err != nil {
		return ids.ID{}, fmt.Errorf("failed to get utxo root: %w", err)
	}

	return stateRoot, nil
}

func (g gForkState) Close() error {
	return errors.Join(
		g.migrationDB.Close(),
		g.singletonDB.Close(),
		g.blockDB.Close(),
		g.blockIDDB.Close(),
		g.txDB.Close(),
		g.utxoIndexDB.Close(),
		g.utxoMerkleDB.Close(),
		g.utxoDB.Close(),
		g.metadataDB.Close(),
		g.stateMerkleDB.Close(),
		g.stateDB.Close(),
		g.baseDB.Close(),
	)
}
