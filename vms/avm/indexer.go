package avm

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errNoTxAtThatIndex   = errors.New("no accepted transaction at that index")
	indexDbPrefix        = []byte("index")
	nextAcceptedIndexKey = []byte("nextAcceptedIndex")
)

type indexer struct {
	nextAcceptedIndex uint64
	db                database.Database
	log               logging.Logger
}

func newIndexer(db database.Database, log logging.Logger) (*indexer, error) {
	i := &indexer{
		db:  db,
		log: log,
	}
	// Find out what the next accepted index is
	nextAcceptedIndexBytes, err := i.db.Get(nextAcceptedIndexKey)
	if err == database.ErrNotFound {
		i.log.Info("returning indexer with next accepted index %d", i.nextAcceptedIndex) // TODO remove
		return i, nil
	} else if err != nil {
		return nil, fmt.Errorf("couldn't get next accepted index from database: %w", err)
	}
	p := wrappers.Packer{Bytes: nextAcceptedIndexBytes}
	i.nextAcceptedIndex = p.UnpackLong()
	if p.Err != nil {
		return nil, fmt.Errorf("couldn't parse next accepted index from bytes: %w", err)
	}
	i.log.Info("returning indexer with next accepted index %d", i.nextAcceptedIndex) // TODO remove
	return i, nil
}

// Index that the given transaction is accepted
// Returned error should be treated as fatal
func (i *indexer) markAccepted(txID ids.ID) error {
	// Create Key Value entry:
	// Next accepted index --> tx at that index
	batch := i.db.NewBatch()
	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(i.nextAcceptedIndex)
	k1 := p.Bytes // todo delete
	if p.Err != nil {
		return fmt.Errorf("couldn't convert next accepted index to bytes: %w", p.Err)
	}
	i.log.Info("mapping %v --> %v", p.Bytes, txID[:]) // TODO remove
	err := batch.Put(p.Bytes, txID[:])
	if err != nil {
		return fmt.Errorf("couldn't put accepted transaction %s into index: %w", txID, err)
	}

	// Increment the next accepted index and store in database
	i.nextAcceptedIndex++
	p = wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(i.nextAcceptedIndex)
	if p.Err != nil {
		return fmt.Errorf("couldn't convert next accepted index to bytes: %w", p.Err)
	}
	i.log.Info("next accepted index is %d (%v)", i.nextAcceptedIndex, p.Bytes) // TODO remove
	err = batch.Put(nextAcceptedIndexKey, p.Bytes)
	if err != nil {
		return fmt.Errorf("couldn't put accepted transaction %s into index: %w", txID, err)
	}

	// Write to underlying data store
	err = batch.Write()
	if err != nil {
		return fmt.Errorf("couldn't write batch: %w", err)
	}

	lastAcc, err := i.db.Get(k1) // todo remove
	if err != nil {
		return err
	}
	nextIndex, err := i.db.Get(nextAcceptedIndexKey) // todo remove
	if err != nil {
		return err
	}
	i.log.Info("last accepted: %v. next accepted index: %v", lastAcc, nextIndex)
	return nil
}

// Returns the [index]th accepted transaction
// For example, if [index] == 0, returns the first accepted transaction
// Returns ids.Empty if no transaction has been accepted at that index
// Returned error should be treated as fatal
func (i *indexer) getAcceptedTxByIndex(index uint64) (ids.ID, error) {
	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(index)
	if p.Err != nil {
		return ids.Empty, fmt.Errorf("couldn't convert index to bytes: %w", p.Err)
	}
	i.log.Info("getting block at index %d (%v)", index, p.Bytes) // TODO remove

	txIDBytes, err := i.db.Get(p.Bytes)
	switch {
	case err == database.ErrNotFound:
		i.log.Info("not found") // TODO remove
		return ids.Empty, nil
	case err != nil:
		return ids.Empty, fmt.Errorf("couldn't read from database: %w", err)
	case len(txIDBytes) != 32:
		// Should never happen
		i.log.Info("not 32 bytes") // TODO remove
		return ids.Empty, fmt.Errorf("expected tx ID to be 32 bytes but was %d", len(txIDBytes))
	}
	var txID ids.ID
	copy(txID[:], txIDBytes)
	return txID, nil
}

type IndexerService struct {
	indexer *indexer
}

type GetAcceptedTxIDByIndex struct {
	Index json.Uint64 `json:"index"`
}

// Send returns the ID of the newly created transaction
func (is *IndexerService) GetAcceptedTxIDByIndex(r *http.Request, args *GetAcceptedTxIDByIndex, reply *api.JSONTxID) error {
	txID, err := is.indexer.getAcceptedTxByIndex(uint64(args.Index))
	if err != nil {
		return err
	}
	if txID == ids.Empty {
		return errNoTxAtThatIndex
	}
	reply.TxID = txID
	return nil
}
