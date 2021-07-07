// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/hashing"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// AddressTxsIndexer Maintains an index of an address --> IDs of transactions that changed that address's balance.
// This includes both transactions that increased the address's balance and those that decreased it.
// A transaction is said to change an address's balance if either hold:
// 1) An input UTXO to the transaction was at least partially owned by the address
// 2) An output of the transaction is at least partially owned by the address
type AddressTxsIndexer interface {
	// Init initializes the indexer returning an error if state is invalid
	Init(bool) error

	// Reset clears unwritten indexer state.
	Reset(ids.ID)

	// AddUTXOs adds given slice of [outputUTXOs] to the indexer state
	AddUTXOs(txID ids.ID, outputUTXOs []*avax.UTXO)

	// AddUTXOsByID adds the given [inputUTXOIDs] to the indexer state.
	// The [getUTXOFn] function is used to get the underlying [avax.UTXO] from each
	// [avax.UTXOID] in the [inputUTXOIDs] slice.
	// Shuts down the node in case of an error
	AddUTXOsByID(getUTXOF func(utxoID *avax.UTXOID) (*avax.UTXO, error), txID ids.ID, inputUTXOIDs []*avax.UTXOID)

	// Write persists the indexer state against the given [txID].
	// Reset() must be called manually to reset the state for next write.
	// Shuts down the node in case of an error
	Write(txID ids.ID)

	// Read returns list of txIDs indexed against the specified [address] and [assetID]
	// Returned slice length will be less than or equal to [pageSize].
	// [cursor] defines the offset to read the entries from
	Read(address ids.ShortID, assetID ids.ID, cursor, pageSize uint64) ([]ids.ID, error)
}

var (
	idxKey        = []byte("idx")
	idxEnabledKey = []byte("addressTxsIdxEnabled")
)

const DatabaseOpErrorExitCode int = 5

// indexer implements AddressTxsIndexer
type indexer struct {
	// txID -> Address -> AssetID --> Present if the address's balance
	// of the asset has changed since last Write
	addressAssetIDTxMap map[ids.ID]map[ids.ShortID]map[ids.ID]struct{}
	db                  *versiondb.Database
	log                 logging.Logger
	metrics             Metrics
	// Called in a goroutine on shutdown
	shutdownF func(int)
}

// addTransferOutput indexes given assetID and any number of addresses linked to the transferOutput
// to the provided vm.addressAssetIDIndex
func (i *indexer) addTransferOutput(txID, assetID ids.ID, addrs []ids.ShortID) {
	for _, address := range addrs {
		if _, exists := i.addressAssetIDTxMap[txID]; !exists {
			i.addressAssetIDTxMap[txID] = make(map[ids.ShortID]map[ids.ID]struct{})
		}

		if _, exists := i.addressAssetIDTxMap[txID][address]; !exists {
			i.addressAssetIDTxMap[txID][address] = make(map[ids.ID]struct{})
		}

		i.addressAssetIDTxMap[txID][address][assetID] = struct{}{}
	}
}

// AddUTXOsByID adds given the given UTXOs to the indexer's unwritten state.
// [getUTXOF] is used to look up UTXOs by their ID.
func (i *indexer) AddUTXOsByID(getUTXOF func(utxoID *avax.UTXOID) (*avax.UTXO, error), txID ids.ID, inputUTXOIDs []*avax.UTXOID) {
	for _, utxoID := range inputUTXOIDs {
		utxo, err := getUTXOF(utxoID)
		if err != nil {
			i.log.Fatal("Error finding UTXO ID %s when indexing transaction %s", utxoID, txID)
			i.shutdownF(DatabaseOpErrorExitCode)
			return // should never happen
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.log.Verbo("skipping utxo %s for indexing because it isn't a secp256k1fx.TransferOutput", utxo.InputID())
			continue
		}

		i.addTransferOutput(txID, utxo.AssetID(), out.Addrs)
	}
}

// AddUTXOs adds given [utxos] to the indexer
func (i *indexer) AddUTXOs(txID ids.ID, utxos []*avax.UTXO) {
	for _, utxo := range utxos {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.log.Verbo("Skipping output utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID())
			continue
		}

		i.addTransferOutput(txID, utxo.AssetID(), out.Addrs)
	}
}

// Write persists unpersisted data.
// Associates all UTXOs in [i.addressAssetIDTxMap] with transaction [txID].
// The database structure is:
// [address]
// |  [assetID]
// |  |
// |  | "idx" => 2 		Running transaction index key, represents the next index
// |  | "0"   => txID1
// |  | "1"   => txID1
func (i *indexer) Write(txID ids.ID) {
	// check if we have data to write for this [txID]
	if _, exists := i.addressAssetIDTxMap[txID]; !exists {
		i.log.Warn("Nothing indexed for txID %s to write", txID)
		return
	}

	// go through all addresses indexed against this [txID]
	// and persist them, maintaining order
	for address, assetIDs := range i.addressAssetIDTxMap[txID] {
		addressPrefixDB := prefixdb.New(address[:], i.db)
		for assetID := range assetIDs {
			assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)

			i.log.Debug("Writing address/AssetID/<index>/txID %s/%s/?/%s", address, assetID, txID)

			var idx uint64
			idxBytes, err := assetPrefixDB.Get(idxKey)
			switch {
			case err != nil && err != database.ErrNotFound:
				// Unexpected error
				i.log.Fatal("Error checking idx value exists when writing txID [%s], address [%s]: %s", txID, address, err)
				i.shutdownF(DatabaseOpErrorExitCode)
				return
			case err == database.ErrNotFound:
				// idx not found; this must be the first entry.
				idx = 0
				idxBytes = make([]byte, wrappers.LongLen)
				binary.BigEndian.PutUint64(idxBytes, idx)
			default:
				// index is found, parse stored [idxBytes]
				idx = binary.BigEndian.Uint64(idxBytes)
				i.log.Verbo("fetched index %d", idx)
			}

			// write the [txID] at the index
			i.log.Debug("Writing address/AssetID/index/txID %s/%s/%d/%s", address, assetID, idx, txID)
			if err := assetPrefixDB.Put(idxBytes, txID[:]); err != nil {
				i.log.Fatal("Failed to save txID [%s], idx [%d] to the address [%s], assetID [%s] prefix DB %s", txID, idx, address, assetID, err)
				i.shutdownF(DatabaseOpErrorExitCode)
				return
			}

			i.log.Debug("Written address/AssetID/index/txID %s/%s/%d/%s", address, assetID, idx, txID)

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				i.log.Fatal("Failed to update index for txID [%s] to the address [%s], assetID [%s] prefix DB: %s", txID, address, assetID, err)
				i.shutdownF(DatabaseOpErrorExitCode)
				return
			}
		}
	}
	// delete already written [txID] from the map
	delete(i.addressAssetIDTxMap, txID)
	i.metrics.numTxsIndexed.Observe(1)
}

// Init initialises indexing, returning error if the state is invalid
func (i *indexer) Init(allowIncomplete bool) error {
	return checkIndexingStatus(i.db, true, allowIncomplete)
}

// checkIndexingStatus checks the indexing status in the database, returning error if the state
// with respect to provided parameters is invalid
func checkIndexingStatus(db *versiondb.Database, enableIndexing, allowIncomplete bool) error {
	// verify whether we've indexed before
	idxEnabled, err := db.Get(idxEnabledKey)
	if err == database.ErrNotFound {
		// we're not allowed incomplete index and we've not indexed before
		// so its ok to proceed
		// save the flag for next time indicating indexing status
		return saveIndexingStateToDB(db, enableIndexing)
	} else if err != nil {
		// some other error happened when reading the database
		return err
	}

	// ok so we have a value stored in the db, lets check its integrity
	if len(idxEnabled) != wrappers.BoolLen {
		// its the wrong size, maybe the database got corrupted?
		return fmt.Errorf("idxEnabled does not have expected size %d, found %d", wrappers.BoolLen, len(idxEnabled))
	}

	// idxEnabled is of expected size, lets see if its true or false
	if idxEnabled[0] == 0 {
		// index was previously enabled
		if enableIndexing && !allowIncomplete {
			// indexing was disabled before, we're asked to enable it but not allowed
			// incomplete entries, we return error
			return fmt.Errorf("indexing is off and incomplete indexing is not allowed, to proceed allow incomplete indexing in config or reindex from genesis")
		} else if enableIndexing {
			// we're asked to enable indexing and allow incomplete indexes
			// save state to db and continue
			return saveIndexingStateToDB(db, enableIndexing)
		}
	} else if idxEnabled[0] == 1 {
		// index was previously disabled
		if !enableIndexing && !allowIncomplete {
			// index is previously enabled, we're asked to disable it but not allowed incomplete indexes
			return fmt.Errorf("cannot disable indexing when incomplete indexes are not allowed, to proceed allow incomplete indexing in config or reset state from genesis")
		} else if !enableIndexing {
			// we're asked to disable indexing and allow incomplete indexes
			// save state to db and continue
			return saveIndexingStateToDB(db, enableIndexing)
		}
	}

	// idxEnabled is true
	// we're not allowed incomplete index AND index is enabled already
	// so we're good to proceed
	return nil
}

// saveIndexingStateToDB saves the provided [enableIndexing] state to [db]
func saveIndexingStateToDB(db *versiondb.Database, enableIndexing bool) error {
	var idxEnabledVal byte
	if enableIndexing {
		idxEnabledVal = 1
	}

	err := db.Put(idxEnabledKey, []byte{idxEnabledVal})
	if err != nil {
		return err
	}

	err = db.Commit()
	return err
}

// Read returns IDs of transactions that changed [address]'s balance of [assetID],
// starting at [cursor], in order of transaction acceptance. e.g. if [cursor] == 1, does
// not return the first transaction that changed the balance. (This is for for pagination.)
// Returns at most [pageSize] elements.
func (i *indexer) Read(address ids.ShortID, assetID ids.ID, cursor, pageSize uint64) ([]ids.ID, error) {
	// setup prefix DBs
	addressTxDB := prefixdb.New(address[:], i.db)
	assetPrefixDB := prefixdb.New(assetID[:], addressTxDB)

	// get cursor in bytes
	cursorBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(cursorBytes, cursor)

	// start reading from the cursor bytes, numeric keys maintain the order (see Write)
	iter := assetPrefixDB.NewIteratorWithStart(cursorBytes)
	defer iter.Release()

	var txIDs []ids.ID
	for uint64(len(txIDs)) < pageSize && iter.Next() {
		// if the key is literally "idx", skip
		if bytes.Equal(idxKey, iter.Key()) {
			continue
		}

		// get the value, make sure its in the right format
		txIDBytes := iter.Value()
		if len(txIDBytes) != hashing.HashLen {
			return nil, fmt.Errorf("invalid tx ID %s", txIDBytes)
		}

		// get the ID and append to our list
		var txID ids.ID
		copy(txID[:], txIDBytes)

		txIDs = append(txIDs, txID)
	}
	return txIDs, nil
}

// Reset resets the entries under given [txID]
func (i *indexer) Reset(txID ids.ID) {
	i.addressAssetIDTxMap[txID] = make(map[ids.ShortID]map[ids.ID]struct{})
}

// NewAddressTxsIndexer Returns a new AddressTxsIndexer.
// The returned indexer ignores UTXOs that are not type secp256k1fx.TransferOutput.
func NewAddressTxsIndexer(db *versiondb.Database, log logging.Logger, m Metrics, shutdownFunc func(int)) AddressTxsIndexer {
	return &indexer{
		addressAssetIDTxMap: make(map[ids.ID]map[ids.ShortID]map[ids.ID]struct{}),
		db:                  db,
		log:                 log,
		metrics:             m,
		shutdownF:           shutdownFunc,
	}
}

type noIndexer struct {
	db *versiondb.Database
}

func NewNoIndexer(db *versiondb.Database) AddressTxsIndexer {
	return &noIndexer{
		db: db,
	}
}

func (i *noIndexer) Init(allowIncomplete bool) error {
	return checkIndexingStatus(i.db, false, allowIncomplete)
}

func (i *noIndexer) AddUTXOsByID(func(utxoID *avax.UTXOID) (*avax.UTXO, error), ids.ID, []*avax.UTXOID) {
}

func (i *noIndexer) AddTransferOutput(ids.ID, ids.ID, *secp256k1fx.TransferOutput) {}

func (i *noIndexer) AddUTXOs(ids.ID, []*avax.UTXO) {}

func (i *noIndexer) Write(ids.ID) {}

func (i *noIndexer) Reset(ids.ID) {}

func (i *noIndexer) Read(ids.ShortID, ids.ID, uint64, uint64) ([]ids.ID, error) {
	return nil, nil
}
