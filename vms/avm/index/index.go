// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database/versiondb"
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
	idxKey        = []byte("idx")
	idxEnabledKey = []byte("addressTxsIdxEnabled")
)

// AddressTxsIndexer maintains information about which transactions changed
// the balances of which addresses. This includes both transactions that
// increase and decrease an address's balance.
// A transaction is said to change an address's balance if either is true:
// 1) A UTXO that the transaction consumes was at least partially owned by the address.
// 2) A UTXO that the transaction produces is at least partially owned by the address.
type AddressTxsIndexer interface {
	// Add must be called after [txID] passes verification.
	// [inputUTXOIDs] are the IDs of UTXOs [txID] consumes.
	// [outputUTXOs] are the UTXOs [txID] creates.
	// [getUTXOF] can be used to look up UTXOs by ID.
	Add(
		txID ids.ID,
		inputUTXOIDs []*avax.UTXOID,
		outputUTXOs []*avax.UTXO,
		getUTXOF func(utxoID *avax.UTXOID) (*avax.UTXO, error),
	)

	// Accept must be called when [txID] is accepted.
	// Persists data about [txID] and what balances it changed.
	// If the error is non-nil, do not persist [txID] to disk as accepted in the VM.
	Accept(txID ids.ID) error

	// Reject must be called when [txID] is rejected.
	// Clears unwritten state about a rejected tx.
	Reject(ids.ID)

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
	db      *versiondb.Database
	// txID -> Address -> AssetID --> exists if the address's balance
	// of the asset is changed by processing tx [txID]
	balanceChanges map[ids.ID]map[ids.ShortID]map[ids.ID]struct{}
	// Called in a goroutine on shutdown. Stops this node.
	shutdownF func(int)
}

// NewIndexer Returns a new AddressTxsIndexer.
// The returned indexer ignores UTXOs that are not type secp256k1fx.TransferOutput.
func NewIndexer(
	db *versiondb.Database,
	log logging.Logger,
	shutdownF func(int),
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
	allowIncompleteIndices bool,
) (AddressTxsIndexer, error) {
	i := &indexer{
		balanceChanges: make(map[ids.ID]map[ids.ShortID]map[ids.ID]struct{}),
		db:             db,
		log:            log,
		shutdownF:      shutdownF,
	}
	// initialize the indexer
	if err := i.init(allowIncompleteIndices); err != nil {
		return nil, err
	}
	// initialize the metrics
	if err := i.metrics.initialize(metricsNamespace, metricsRegisterer); err != nil {
		return nil, err
	}
	// Commit anything we wrote in [init]
	if err := i.db.Commit(); err != nil {
		return nil, err
	}
	return i, nil
}

// add marks that [txID] changes the balance of [assetID] for [addrs]
// This data is either written in Accept() or cleared in Reject()
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
) {
	utxos := outputUTXOs
	for _, utxoID := range inputUTXOIDs {
		utxo, err := getUTXOF(utxoID)
		if err != nil { // should never happen
			i.log.Fatal("error finding UTXO %s: %s", utxoID, err)
			// Shut down the node
			go i.shutdownF(DatabaseOpErrorExitCode)
			return
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
				i.log.Fatal("error indexing txID %s: %s", txID, err)
				go i.shutdownF(DatabaseOpErrorExitCode)
				return err
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
				i.log.Fatal("failed to write txID while indexing %s: %s", txID, err)
				go i.shutdownF(DatabaseOpErrorExitCode)
				return err
			}

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				i.log.Fatal("failed to write index txID while indexing %s: %s", txID, err)
				go i.shutdownF(DatabaseOpErrorExitCode)
				return err
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

// Reject clears data about which balances [txID] changed.
func (i *indexer) Reject(txID ids.ID) {
	i.balanceChanges[txID] = make(map[ids.ShortID]map[ids.ID]struct{})
}

// Init initialises indexing, returning error if the state is invalid
func (i *indexer) init(allowIncomplete bool) error {
	return checkIndexingStatus(i.db, true, allowIncomplete)
}

// checkIndexingStatus checks the indexing status in the database, returning error if the state
// with respect to provided parameters is invalid
func checkIndexingStatus(db database.KeyValueReaderWriter, enableIndexing, allowIncomplete bool) error {
	// verify whether we've indexed before
	idxEnabled, err := database.GetBool(db, idxEnabledKey)
	if err == database.ErrNotFound {
		// we're not allowed incomplete index and we've not indexed before
		// so its ok to proceed
		// save the flag for next time indicating indexing status
		return database.PutBool(db, idxEnabledKey, enableIndexing)
	} else if err != nil {
		// some other error happened when reading the database
		return err
	}

	if !idxEnabled {
		// index was previously enabled
		if enableIndexing && !allowIncomplete {
			// indexing was disabled before, we're asked to enable it but not allowed
			// incomplete entries, we return error
			return fmt.Errorf("indexing is off and incomplete indexing is not allowed, to proceed allow incomplete indexing in config or reindex from genesis")
		} else if enableIndexing {
			// we're asked to enable indexing and allow incomplete indexes
			// save state to db and continue
			return database.PutBool(db, idxEnabledKey, enableIndexing)
		}
	} else {
		// index was previously disabled
		if !enableIndexing && !allowIncomplete {
			// index is previously enabled, we're asked to disable it but not allowed incomplete indexes
			return fmt.Errorf("cannot disable indexing when incomplete indexes are not allowed, to proceed allow incomplete indexing in config or reset state from genesis")
		} else if !enableIndexing {
			// we're asked to disable indexing and allow incomplete indexes
			// save state to db and continue
			return database.PutBool(db, idxEnabledKey, enableIndexing)
		}
	}

	// idxEnabled is true
	// we're not allowed incomplete index AND index is enabled already
	// so we're good to proceed
	return nil
}

type noIndexer struct{}

func NewNoIndexer(db *versiondb.Database, allowIncomplete bool) (AddressTxsIndexer, error) {
	if err := checkIndexingStatus(db, false, allowIncomplete); err != nil {
		return nil, err
	}

	return &noIndexer{}, nil
}

func (i *noIndexer) Add(ids.ID, []*avax.UTXOID, []*avax.UTXO, func(utxoID *avax.UTXOID) (*avax.UTXO, error)) {
}

func (i *noIndexer) Accept(ids.ID) error {
	return nil
}

func (i *noIndexer) Reject(ids.ID) {}

func (i *noIndexer) Read(ids.ShortID, ids.ID, uint64, uint64) ([]ids.ID, error) {
	return nil, nil
}
