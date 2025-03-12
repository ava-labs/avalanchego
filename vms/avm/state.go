package avm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/prometheus/client_golang/prometheus"

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
	_ state.State           = (*gForkState)(nil)
	_ StateMigrationFactory = (*NoStateMigrationFactory)(nil)
	_ StateMigration        = (*noStateMigration)(nil)
	_ StateMigrationFactory = (*GForkStateMigrationFactory)(nil)
	_ StateMigration        = (*gForkStateMigration)(nil)

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
		trackChecksums bool,
	) StateMigration
}

type StateMigration interface {
	Migrate(
		ctx context.Context,
		prevState state.State,
		prevBaseDB *versiondb.Database,
		prevTXDB database.Database,
		prevBlockIDDB database.Database,
		prevBlockDB database.Database,
		prevSingletonDB database.Database,
		nextDB database.Database, // TODO should i pass in versiondb instead? Or just use the prevBaseDB for both
	) (state.State, error)
}

type NoStateMigrationFactory struct{}

func (n NoStateMigrationFactory) New(
	_ logging.Logger,
	parser block.Parser,
	metrics prometheus.Registerer,
	trackChecksums bool,
) StateMigration {
	return noStateMigration{
		parser:         parser,
		metrics:        metrics,
		trackChecksums: trackChecksums,
	}
}

type noStateMigration struct {
	parser         block.Parser
	metrics        prometheus.Registerer
	trackChecksums bool
}

func (n noStateMigration) Migrate(
	_ context.Context,
	prevState state.State,
	_ *versiondb.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
	_ database.Database,
) (state.State, error) {
	return prevState, nil
}

type GForkStateMigrationFactory struct {
	CommitFrequency int
}

func (g GForkStateMigrationFactory) New(
	log logging.Logger,
	parser block.Parser,
	metrics prometheus.Registerer,
	trackChecksums bool,
) StateMigration {
	return &gForkStateMigration{
		commitFrequency: g.CommitFrequency,
		log:             log,
		parser:          parser,
		metrics:         metrics,
		trackChecksums:  trackChecksums,
	}
}

type gForkStateMigration struct {
	commitFrequency int
	log             logging.Logger
	parser          block.Parser
	metrics         prometheus.Registerer
	trackChecksums  bool

	updates int
}

func (g *gForkStateMigration) Migrate(
	ctx context.Context,
	prevUTXOState state.State,
	prevBaseDB *versiondb.Database,
	prevTXDB database.Database,
	prevBlockIDDB database.Database,
	prevBlockDB database.Database,
	prevSingletonDB database.Database,
	nextDB database.Database,
) (state.State, error) {
	next, err := newGForkState(ctx, nextDB, g.parser, g.metrics, g.trackChecksums)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state: %w", err)
	}

	done, ok, err := next.getMigrationStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get state migration status: %w", err)
	}

	if done {
		g.log.Debug("skipping state migration")
		return next, nil
	}

	if ok {
		g.log.Debug("resuming state migration")
	} else {
		if err := next.putMigrationStatus(false); err != nil {
			return nil, fmt.Errorf("failed to put migration status: %w", err)
		}

		g.log.Debug("starting state migration")
	}

	// TODO i think we only need one commit call since we share the same
	// version db
	// TODO test progress tracking
	lastMigratedUTXO, _, err := next.getLastMigratedUTXO()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated UTXO: %w", err)
	}

	for utxo, err := range prevUTXOState.UTXOs(lastMigratedUTXO) {
		if err != nil {
			return nil, fmt.Errorf("failed to get utxo: %w", err)
		}

		next.AddUTXO(utxo)
		prevUTXOState.DeleteUTXO(utxo.InputID())

		if err := next.putLastMigratedUTXO(utxo.InputID()); err != nil {
			return nil, fmt.Errorf("failed to put last migrated utxo: %w", err)
		}

		ok, err := g.updateAndCommit(next)
		if err != nil {
			return nil, fmt.Errorf("failed to commit next state: %w", err)
		}
		if ok {
			if err := prevBaseDB.Commit(); err != nil {
				return nil, fmt.Errorf("failed to commit previous db: %w", err)
			}
		}
	}

	g.log.Debug("migrating txs")
	var startingTxKey []byte
	lastMigratedTx, ok, err := next.getLastMigratedTx()
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

		_, err := g.updateAndCommit(next)
		if err != nil {
			return nil, fmt.Errorf("failed to commit next state: %w", err)
		}

	}

	g.log.Debug("migrating blocks")

	var startingBlkKey []byte
	lastMigratedBlk, ok, err := next.getLastMigratedBlock()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated block: %w", err)
	}

	if ok {
		startingBlkKey = lastMigratedBlk[:]
	}

	for itr := prevBlockDB.NewIteratorWithStart(startingBlkKey); itr.Next(); {
		if err := next.blockDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block: %w", err)
		}

	}

	g.log.Debug("migrating height index")

	var startingBlkHeightKey []byte
	lastMigratedBlkHeight, ok, err := next.getLastMigratedBlockHeight()
	if err != nil {
		return nil, fmt.Errorf("failed to get last migrated block height: %w", err)
	}

	if ok {
		// TODO skips
		startingBlkHeightKey = database.PackUInt64(lastMigratedBlkHeight)
	}

	for itr := prevBlockIDDB.NewIteratorWithStart(startingBlkHeightKey); itr.Next(); {
		if err := next.blockIDDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate block height: %w", err)
		}
	}

	g.log.Debug("migrating singletons")
	for itr := prevSingletonDB.NewIterator(); itr.Next(); {
		fmt.Printf("key: %s value: %s\n", itr.Key(), itr.Value())
		if err := next.singletonDB.Put(itr.Key(), itr.Value()); err != nil {
			return nil, fmt.Errorf("failed to migrate singleton: %w", err)
		}
	}

	g.log.Debug("migration complete")

	if err := next.putMigrationStatus(true); err != nil {
		return nil, fmt.Errorf("failed to put migration status: %w", err)
	}

	return next, next.Commit()
}

func (g *gForkStateMigration) updateAndCommit(next state.State) (bool, error) {
	g.updates++
	if g.updates%g.commitFrequency == 0 {
		if err := next.Commit(); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

func newGForkState(
	ctx context.Context,
	db database.Database,
	parser block.Parser,
	metrics prometheus.Registerer,
	trackChecksums bool,
) (gForkState, error) {
	versionDB := versiondb.New(db)

	merkleDBConfig := merkledb.NewConfig()

	stateMerkleDBConfig := merkleDBConfig
	stateMerkleDBConfig.Namespace = "state"

	// Data required for execution is stored under the state prefix
	stateDB := prefixdb.New([]byte("state"), versionDB)
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

	// TODO naming?
	// All other data is stored under the metadata prefix
	metadataDB := prefixdb.New([]byte("metadata"), versionDB)
	utxoIndexDB := prefixdb.New([]byte("utxo_index"), metadataDB)
	txDB := prefixdb.New([]byte("tx"), metadataDB)
	blockIDDB := prefixdb.New([]byte("block_id"), metadataDB)
	blockDB := prefixdb.New([]byte("block"), metadataDB)
	singletonDB := prefixdb.New([]byte("singleton"), metadataDB)
	migrationDB := prefixdb.New([]byte("migration"), metadataDB)

	s, err := state.NewWithFormat(
		"v2",
		versionDB,
		utxoMerkleDB,
		utxoIndexDB,
		txDB,
		blockIDDB,
		blockDB,
		singletonDB,
		parser,
		metrics,
		trackChecksums,
	)
	if err != nil {
		return gForkState{}, fmt.Errorf("failed to initialize state: %w", err)
	}

	return gForkState{
		state:         s,
		stateMerkleDB: stateMerkleDB,
		versionDB:     versionDB,
		metadataDB:    metadataDB,
		utxoDB:        utxoMerkleDB,
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
	state         state.State
	stateMerkleDB merkledb.MerkleDB

	versionDB   *versiondb.Database
	metadataDB  database.Database
	utxoDB      merkledb.MerkleDB
	utxoIndexDB database.Database
	txDB        database.Database
	blockIDDB   database.Database
	blockDB     database.Database
	singletonDB database.Database
	// Metadata for migrated keys
	migrationDB database.Database
}

func (g gForkState) getMigrationStatus() (bool, bool, error) {
	ok, err := database.GetBool(g.migrationDB, migrationStatusKey)
	if errors.Is(err, database.ErrNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}

	return ok, true, nil
}

func (g gForkState) putMigrationStatus(done bool) error {
	return database.PutBool(g.migrationDB, migrationStatusKey, done)
}

func (g gForkState) getLastMigratedUTXO() (ids.ID, bool, error) {
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

func (g gForkState) putLastMigratedUTXO(utxoID ids.ID) error {
	return g.migrationDB.Put(lastMigratedUTXOKey, utxoID[:])
}

func (g gForkState) getLastMigratedTx() (ids.ID, bool, error) {
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

func (g gForkState) putLastMigratedTx(txID ids.ID) error {
	if err := g.migrationDB.Put(lastMigratedTxKey, txID[:]); err != nil {
		return err
	}

	return nil
}

func (g gForkState) getLastMigratedBlock() (ids.ID, bool, error) {
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

func (g gForkState) putLastMigratedBlock(blkID ids.ID) error {
	return g.migrationDB.Put(lastMigratedBlockKey, blkID[:])
}

func (g gForkState) getLastMigratedBlockHeight() (uint64, bool, error) {
	height, err := database.GetUInt64(g.migrationDB, lastMigratedBlockHeightKey)
	if errors.Is(err, database.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return height, true, nil
}

func (g gForkState) putLastMigratedBlockHeight(height uint64) error {
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

func (g gForkState) InitializeChainState(ids.ID, time.Time) error {
	return errors.New("cannot initialize")
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
	return g.state.Close()
}
