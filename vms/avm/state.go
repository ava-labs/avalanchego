package avm

import (
	"context"
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

var _ state.State = (*gForkState)(nil)

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

	// Holds data required for tx execution
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

	// Holds all other data
	metadataDB := prefixdb.New([]byte("metadata"), versionDB)
	txDB := prefixdb.New([]byte("tx"), metadataDB)
	blockIDDB := prefixdb.New([]byte("block_id"), metadataDB)
	blockDB := prefixdb.New([]byte("block"), metadataDB)
	singletonDB := prefixdb.New([]byte("singleton"), metadataDB)

	s, err := state.NewWithFormat(
		versionDB,
		utxoMerkleDB,
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
	}, nil
}

// TODO decompose this struct into state used/not used during execution
// TODO move this into state/ once the G fork is completed
type gForkState struct {
	state         state.State
	stateMerkleDB merkledb.MerkleDB
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

func (g gForkState) UTXOIDs(addr []byte, previous ids.ID, limit int) ([]ids.ID, error) {
	return g.state.UTXOIDs(addr, previous, limit)
}

func (g gForkState) UTXOs() iter.Seq2[*avax.UTXO, error] {
	return g.state.UTXOs()
}

func (g gForkState) IsInitialized() (bool, error) {
	return g.state.IsInitialized()
}

func (g gForkState) SetInitialized() error {
	return g.state.SetInitialized()
}

func (g gForkState) InitializeChainState(stopVertexID ids.ID, genesisTimestamp time.Time) error {
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

func (g gForkState) Checksums(ctx context.Context) (ids.ID, error) {
	stateRoot, err := g.stateMerkleDB.GetMerkleRoot(ctx)
	if err != nil {
		return ids.ID{}, fmt.Errorf("failed to get utxo root: %w", err)
	}

	return stateRoot, nil
}

func (g gForkState) Txs() iter.Seq2[*txs.Tx, error] {
	return g.state.Txs()
}

func (g gForkState) Blocks() iter.Seq2[block.Block, error] {
	return g.state.Blocks()
}

func (g gForkState) Close() error {
	return g.state.Close()
}

func migrateDB[T any](
	seq iter.Seq2[T, error],
	accept func(t T),
	s *gForkState,
	freq int,
) error {
	i := 0
	for e, err := range seq {
		if err != nil {
			return err
		}

		accept(e)
		i++

		if i%freq != 0 {
			continue
		}

		if err := s.Commit(); err != nil {
			return fmt.Errorf("failed to commit db: %w", err)
		}
	}

	if err := s.Commit(); err != nil {
		return fmt.Errorf("failed to commit db: %w", err)
	}

	return nil
}

// TODO move to vm?
func Migrate(log logging.Logger, prev state.State, next gForkState,
	commitFrequency int) error {
	log.Debug("migrating utxos")
	if err := migrateDB[*avax.UTXO](
		prev.UTXOs(),
		func(utxo *avax.UTXO) { next.AddUTXO(utxo) },
		next,
		commitFrequency,
	); err != nil {
		return fmt.Errorf("failed to migrate utxos: %w", err)
	}

	log.Debug("migrating txs")
	if err := migrateDB[*txs.Tx](
		prev.Txs(),
		func(tx *txs.Tx) { next.AddTx(tx) },
		next,
		commitFrequency,
	); err != nil {
		return fmt.Errorf("failed to migrate txs: %w", err)
	}

	log.Debug("migrating blocks")
	if err := migrateDB[block.Block](
		prev.Blocks(),
		func(blk block.Block) {
			next.AddBlock(blk)
		},
		next,
		commitFrequency,
	); err != nil {
		return fmt.Errorf("failed to migrate blocks: %w", err)
	}

	log.Debug("migrating singletons")
	next.SetTimestamp(prev.GetTimestamp())
	ok, err := prev.IsInitialized()
	if err != nil {
		return fmt.Errorf("failed to get initialized flag: %w", err)
	}
	if ok {
		if err := next.SetInitialized(); err != nil {
			return fmt.Errorf("failed to set initialized flag: %w", err)
		}
	}

	next.SetLastAccepted(prev.GetLastAccepted())

	if err := next.Commit(); err != nil {
		return fmt.Errorf("failed to commit db: %w", err)
	}

	log.Debug("migration complete")

	return nil
}
