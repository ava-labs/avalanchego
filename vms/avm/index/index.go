// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const DatabaseOpErrorExitCode int = 5

var (
	idxKey         = []byte("idx")
	idxCompleteKey = []byte("complete")
)

// AddressTxsIndexer maintains information about which transactions changed
// the balances of which addresses. This includes both transactions that
// increase and decrease an address's balance.
// A transaction is said to change an address's balance if either is true:
// 1) A UTXO that the transaction consumes was at least partially owned by the address.
// 2) A UTXO that the transaction produces is at least partially owned by the address.
type AddressTxsIndexer interface {
	// Add is called during [txID]'s SemanticVerify.
	// [inputUTXOIDs] are the IDs of UTXOs [txID] consumes.
	// [outputUTXOs] are the UTXOs [txID] creates.
	// [getUTXOF] can be used to look up UTXOs by ID.
	// If the error is non-nil, do not persist [txID] to disk as accepted in the VM,
	// and shut down this chain.
	Add(
		txID ids.ID,
		inputUTXOIDs []*avax.UTXOID,
		outputUTXOs []*avax.UTXO,
		getUTXOF func(utxoID *avax.UTXOID) (*avax.UTXO, error),
	) error

	// Accept is called when [txID] is accepted.
	// Persists data about [txID] and what balances it changed.
	// If the error is non-nil, do not persist [txID] to disk as accepted in the VM,
	// and shut down this chain.
	Accept(txID ids.ID) error

	// Clear is called when [txID] is rejected or fails verification.
	// Clears unwritten state about the tx.
	Clear(ids.ID)

	// Read returns the IDs of transactions that changed [address]'s balance of [assetID].
	// The returned transactions are in order of increasing acceptance time.
	// The length of the returned slice <= [pageSize].
	// [cursor] is the offset to start reading from.
	Read(address ids.ShortID, assetID ids.ID, cursor, pageSize uint64) ([]ids.ID, error)
}

// indexer implements AddressTxsIndexer
type indexer struct {
	log     logging.Logger
	metrics metrics
	db      database.Database
	// txID -> Address -> AssetID --> exists if the address's balance
	// of the asset is changed by processing tx [txID]
	balanceChanges map[ids.ID]map[ids.ShortID]map[ids.ID]struct{}
}

// NewIndexer Returns a new AddressTxsIndexer.
// The returned indexer ignores UTXOs that are not type secp256k1fx.TransferOutput.
func NewIndexer(
	db database.Database,
	log logging.Logger,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
	allowIncompleteIndices bool,
) (AddressTxsIndexer, error) {
	i := &indexer{
		balanceChanges: make(map[ids.ID]map[ids.ShortID]map[ids.ID]struct{}),
		db:             db,
		log:            log,
	}
	// initialize the indexer
	if err := i.init(allowIncompleteIndices); err != nil {
		return nil, err
	}
	// initialize the metrics
	if err := i.metrics.initialize(metricsNamespace, metricsRegisterer); err != nil {
		return nil, err
	}
	return i, nil
}

// add marks that [txID] changes the balance of [assetID] for [addrs]
// This data is either written in Accept() or cleared in Clear()
func (i *indexer) add(txID, assetID ids.ID, addrs []ids.ShortID) {
	for _, address := range addrs {
		if _, exists := i.balanceChanges[txID]; !exists {
			i.balanceChanges[txID] = make(map[ids.ShortID]map[ids.ID]struct{})
		}
		if _, exists := i.balanceChanges[txID][address]; !exists {
			i.balanceChanges[txID][address] = make(map[ids.ID]struct{})
		}
		i.balanceChanges[txID][address][assetID] = struct{}{}
	}
}

// See AddressTxsIndexer
// TODO return an error here
func (i *indexer) Add(
	txID ids.ID,
	inputUTXOIDs []*avax.UTXOID,
	outputUTXOs []*avax.UTXO,
	getUTXOF func(utxoID *avax.UTXOID) (*avax.UTXO, error),
) error {
	utxos := outputUTXOs
	for _, utxoID := range inputUTXOIDs {
		utxo, err := getUTXOF(utxoID)
		if err != nil { // should never happen
			return fmt.Errorf("error finding UTXO %s: %s", utxoID, err)
		}
		utxos = append(utxos, utxo)
	}
	for _, utxo := range utxos {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.log.Verbo("skipping UTXO %s for indexing", utxo.InputID())
			continue
		}
		i.add(txID, utxo.AssetID(), out.Addrs)
	}
	return nil
}

// Accept persists which balances [txID] changed.
// Associates all UTXOs in [i.balanceChanges] with transaction [txID].
// The database structure is:
// [address]
// |  [assetID]
// |  |
// |  | "idx" => 2 		Running transaction index key, represents the next index
// |  | "0"   => txID1
// |  | "1"   => txID1
// See AddressTxsIndexer
func (i *indexer) Accept(txID ids.ID) error {
	for address, assetIDs := range i.balanceChanges[txID] {
		addressPrefixDB := prefixdb.New(address[:], i.db)
		for assetID := range assetIDs {
			assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)

			var idx uint64
			idxBytes, err := assetPrefixDB.Get(idxKey)
			switch {
			case err != nil && err != database.ErrNotFound:
				// Unexpected error
				return fmt.Errorf("error indexing txID %s: %s", txID, err)
			case err == database.ErrNotFound:
				// idx not found; this must be the first entry.
				idx = 0
				idxBytes = make([]byte, wrappers.LongLen)
				binary.BigEndian.PutUint64(idxBytes, idx)
			default:
				// index is found, parse stored [idxBytes]
				idx = binary.BigEndian.Uint64(idxBytes)
			}

			// write the [txID] at the index
			i.log.Verbo("writing address/assetID/index/txID %s/%s/%d/%s", address, assetID, idx, txID)
			if err := assetPrefixDB.Put(idxBytes, txID[:]); err != nil {
				return fmt.Errorf("failed to write txID while indexing %s: %s", txID, err)
			}

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				return fmt.Errorf("failed to write index txID while indexing %s: %s", txID, err)
			}
		}
	}
	// delete already written [txID] from the map
	delete(i.balanceChanges, txID)
	i.metrics.numTxsIndexed.Observe(1)
	return nil
}

// Read returns IDs of transactions that changed [address]'s balance of [assetID],
// starting at [cursor], in order of transaction acceptance. e.g. if [cursor] == 1, does
// not return the first transaction that changed the balance. (This is for for pagination.)
// Returns at most [pageSize] elements.
// See AddressTxsIndexer
func (i *indexer) Read(address ids.ShortID, assetID ids.ID, cursor, pageSize uint64) ([]ids.ID, error) {
	// setup prefix DBs
	addressTxDB := prefixdb.New(address[:], i.db)
	assetPrefixDB := prefixdb.New(assetID[:], addressTxDB)

	// get cursor in bytes
	cursorBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(cursorBytes, cursor)

	// start reading from the cursor bytes, numeric keys maintain the order (see Accept)
	iter := assetPrefixDB.NewIteratorWithStart(cursorBytes)
	defer iter.Release()

	var txIDs []ids.ID
	for uint64(len(txIDs)) < pageSize && iter.Next() {
		if bytes.Equal(idxKey, iter.Key()) {
			// This key has the next index to use, not a tx ID
			continue
		}

		// get the value, make sure its in the right format
		txIDBytes := iter.Value()
		if len(txIDBytes) != hashing.HashLen {
			return nil, fmt.Errorf("invalid tx ID %s", txIDBytes)
		}

		// get the  txID and append to our list
		var txID ids.ID
		copy(txID[:], txIDBytes)
		txIDs = append(txIDs, txID)
	}
	return txIDs, nil
}

// Clear clears data about which balances [txID] changed.
func (i *indexer) Clear(txID ids.ID) {
	i.balanceChanges[txID] = make(map[ids.ShortID]map[ids.ID]struct{})
}

// Init initialises indexing, returning error if the state is invalid
func (i *indexer) init(allowIncomplete bool) error {
	return checkIndexStatus(i.db, true, allowIncomplete)
}

// checkIndexStatus checks the indexing status in the database, returning error if the state
// with respect to provided parameters is invalid
func checkIndexStatus(db database.KeyValueReaderWriter, enableIndexing, allowIncomplete bool) error {
	// verify whether the index is complete.
	idxComplete, err := database.GetBool(db, idxCompleteKey)
	if err == database.ErrNotFound {
		// We've not run before. Mark whether indexing is enabled this run.
		return database.PutBool(db, idxCompleteKey, enableIndexing)
	} else if err != nil {
		return err
	}

	if !idxComplete { // In a previous run, we did not index so it's incomplete.
		if enableIndexing && !allowIncomplete {
			// indexing was disabled before but now we want to index.
			return errors.New("running would create incomplete index. Allow incomplete indices or re-sync from genesis with indexing enabled")
		}
		// either indexing is disabled, or incomplete indices are ok, so we don't care that index is incomplete
		return nil
	}

	// the index is complete
	if !enableIndexing { // indexing is disabled this run
		if !allowIncomplete {
			return errors.New("running would create incomplete index. Allow incomplete indices or enable indexing")
		}
		// running without indexing makes it incomplete
		return database.PutBool(db, idxCompleteKey, false)
	}
	return nil
}

type noIndexer struct{}

func NewNoIndexer(db database.Database, allowIncomplete bool) (AddressTxsIndexer, error) {
	if err := checkIndexStatus(db, false, allowIncomplete); err != nil {
		return nil, err
	}
	return &noIndexer{}, nil
}

func (i *noIndexer) Add(ids.ID, []*avax.UTXOID, []*avax.UTXO, func(utxoID *avax.UTXOID) (*avax.UTXO, error)) error {
	return nil
}

func (i *noIndexer) Accept(ids.ID) error {
	return nil
}

func (i *noIndexer) Clear(ids.ID) {}

func (i *noIndexer) Read(ids.ShortID, ids.ID, uint64, uint64) ([]ids.ID, error) {
	return nil, nil
}
