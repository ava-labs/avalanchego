package avm

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// Maximum number of transaction IDs that can be fetched at a time in
	// GetAcceptedTxIDRange
	maxFetchedByRange = 1024
)

var (
	errNoTxAtThatIndex = errors.New("no accepted transaction at that index")
	errNoTxs           = errors.New("no transactions have been accepted")
	indexDbPrefix      = []byte("index")

	// Maps to the byte representation of the next accepted index
	nextAcceptedIndexKey []byte = []byte("next")
)

// indexer indexes all accepted transactions by the order in which they were accepted
type indexer struct {
	// The index of the next accepted transaction
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
		// Couldn't find it in the database. Must not have accepted any txs in previous runs.
		i.log.Info("next accepted index %d", i.nextAcceptedIndex)
		return i, nil
	} else if err != nil {
		return nil, fmt.Errorf("couldn't get next accepted index from database: %w", err)
	}
	p := wrappers.Packer{Bytes: nextAcceptedIndexBytes}
	i.nextAcceptedIndex = p.UnpackLong()
	if p.Err != nil {
		return nil, fmt.Errorf("couldn't parse next accepted index from bytes: %w", err)
	}
	i.log.Info("next accepted index %d", i.nextAcceptedIndex)
	return i, nil
}

// Index that the given transaction is accepted
// Returned error should be treated as fatal
func (i *indexer) markAccepted(txID ids.ID) error {
	batch := i.db.NewBatch()

	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(i.nextAcceptedIndex)
	if p.Err != nil {
		return fmt.Errorf("couldn't convert next accepted index to bytes: %w", p.Err)
	}
	// Next accepted index --> tx at that index
	err := batch.Put(p.Bytes, txID[:])
	if err != nil {
		return fmt.Errorf("couldn't put accepted transaction %s into index: %w", txID, err)
	}

	// Increment the next accepted index and persist last accepted index
	i.nextAcceptedIndex++
	p = wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(i.nextAcceptedIndex)
	if p.Err != nil {
		return fmt.Errorf("couldn't convert next accepted index to bytes: %w", p.Err)
	}
	err = batch.Put(nextAcceptedIndexKey, p.Bytes)
	if err != nil {
		return fmt.Errorf("couldn't put accepted transaction %s into index: %w", txID, err)
	}

	// Write to underlying data store
	err = batch.Write()
	if err != nil {
		return fmt.Errorf("couldn't write batch: %w", err)
	}
	return nil
}

// Returns the [index]th accepted transaction
// For example, if [index] == 0, returns the first accepted transaction
// If [index] == 1, returns the second accepted transaction, etc.
func (i *indexer) getAcceptedTxByIndex(index uint64) (ids.ID, error) {
	lastAcceptedIndex, ok := i.lastAcceptedIndex()
	if !ok || index > lastAcceptedIndex {
		return ids.Empty, errNoTxAtThatIndex
	}

	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(index)
	if p.Err != nil {
		return ids.Empty, fmt.Errorf("couldn't convert index to bytes: %w", p.Err)
	}

	txIDBytes, err := i.db.Get(p.Bytes)
	switch {
	case err == database.ErrNotFound:
		return ids.Empty, errNoTxAtThatIndex
	case err != nil:
		i.log.Error("couldn't read transaction ID from database: %w", err)
		return ids.Empty, fmt.Errorf("couldn't read from database: %w", err)
	case len(txIDBytes) != 32:
		// Should never happen
		i.log.Error("expected tx ID to be 32 bytes but is %d", len(txIDBytes))
		return ids.Empty, fmt.Errorf("expected tx ID to be 32 bytes but is %d", len(txIDBytes))
	}
	var txID ids.ID
	copy(txID[:], txIDBytes)
	return txID, nil
}

// Returns:
// 1) The index of the most recently accepted transaction,
//    or 0 if no transactions have been accepted
// 2) Whether at least 1 transaction has been accepted
func (i *indexer) lastAcceptedIndex() (uint64, bool) {
	if i.nextAcceptedIndex == 0 {
		return 0, false
	}
	return i.nextAcceptedIndex - 1, true
}

// Return the transactions at index [start], [start+1], ... , [start+n-1]
// If [n] == 0, returns nil, nil
// If [n] > [maxFetchedByRange], returns an error
// If the [start] > the last accepted index, returns an error (unless the above applies)
// If we run out of transactions, returns the ones fetched before running out
func (i *indexer) getAcceptedTxRange(startIndex uint64, n uint64) ([]ids.ID, error) {
	// Check arguments for validity
	if n == 0 {
		return nil, nil
	} else if n > maxFetchedByRange {
		return nil, fmt.Errorf("requested %d but maximum page size is %d", n, maxFetchedByRange)
	}
	lastAcceptedIndex, ok := i.lastAcceptedIndex()
	if !ok {
		return nil, fmt.Errorf("start index (%d) > last accepted index (-1)", startIndex)
	} else if startIndex > lastAcceptedIndex {
		return nil, fmt.Errorf("start index (%d) > last accepted index (%d)", startIndex, lastAcceptedIndex)
	}

	// Convert start index to bytes
	p := wrappers.Packer{MaxSize: wrappers.LongLen}
	p.PackLong(startIndex)
	if p.Err != nil {
		return nil, fmt.Errorf("couldn't convert start index to bytes: %w", p.Err)
	}

	// Calculate the size of the needed slice
	var txIDs []ids.ID
	lastIndex := math.Min64(startIndex+n, lastAcceptedIndex)
	// [lastIndex] is always >= [startIndex] so this is safe.
	// [n] is limited to [maxFetchedByRange] so [txIDs] can't be crazy big.
	txIDs = make([]ids.ID, 0, int(lastIndex)-int(startIndex)+1)

	iter := i.db.NewIteratorWithStart(p.Bytes)
	defer iter.Release()

	for len(txIDs) < int(n) && iter.Next() {
		if bytes.Equal(iter.Key(), nextAcceptedIndexKey) {
			// Ignore the one non-index value in the database
			continue
		}
		txIDBytes := iter.Value()
		if len(txIDBytes) != 32 {
			// Should never happen
			return nil, fmt.Errorf("expected tx ID to be 32 bytes but is %d", len(txIDBytes))
		}
		var txID ids.ID
		copy(txID[:], txIDBytes)
		txIDs = append(txIDs, txID)
	}
	return txIDs, nil
}

// IndexerService returns data about accepted transactions
type IndexerService struct {
	indexer *indexer
}

// GetAcceptedTxByIndexArgs ...
type GetAcceptedTxIDByIndexArgs struct {
	Index json.Uint64 `json:"index"`
}

// GetAcceptedTxIDByIndex returns the ID of the transaction at [args.Index]
func (is *IndexerService) GetAcceptedTxIDByIndex(r *http.Request, args *GetAcceptedTxIDByIndexArgs, reply *api.JSONTxID) error {
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

// LastAcceptedIndexReply ...
type LastAcceptedIndexReply struct {
	Index json.Uint64 `json:"index"`
}

// LastAcceptedIndex returns the index of the most recently accepted transaction.
// This is 1 less than the total number of accepted transactions.
// Returns [errNoTxs] if no transactions have been accepted.
func (is *IndexerService) LastAcceptedIndex(r *http.Request, args *GetAcceptedTxIDByIndexArgs, reply *LastAcceptedIndexReply) error {
	lastIndex, ok := is.indexer.lastAcceptedIndex()
	if !ok {
		return errNoTxs
	}
	reply.Index = json.Uint64(lastIndex)
	return nil
}

// GetAcceptedTxIDRangeArgs ...
type GetAcceptedTxIDRangeArgs struct {
	StartIndex json.Uint64 `json:"index"`
	NumToFetch json.Uint64 `json:"numToFetch"`
}

// GetAcceptedTxIDRangeArgs ...
type GetAcceptedTxIDRangeReply struct {
	TxIDs []struct {
		json.Uint64 `json:"index"`
		ids.ID      `json:"txID"`
	} `json:"txs"`
}

// GetAcceptedTxIDByIndex returns the transactions at index [startIndex], [startIndex+1], ... , [startIndex+n-1]
// If [n] == 0, returns an empty response (i.e. null).
// If [startIndex] > the last accepted index, returns an error (unless the above apply.)
// If [n] > [maxFetchedByRange], returns an error.
// If we run out of transactions, returns the ones fetched before running out.
func (is *IndexerService) GetAcceptedTxIDRange(r *http.Request, args *GetAcceptedTxIDRangeArgs, reply *GetAcceptedTxIDRangeReply) error {
	txIDs, err := is.indexer.getAcceptedTxRange(uint64(args.StartIndex), uint64(args.NumToFetch))
	if err != nil {
		return err
	}
	nextIndex := args.StartIndex
	for _, txID := range txIDs {
		reply.TxIDs = append(
			reply.TxIDs,
			struct {
				json.Uint64 `json:"index"`
				ids.ID      `json:"txID"`
			}{
				nextIndex,
				txID,
			},
		)
		nextIndex++
	}
	return nil
}
