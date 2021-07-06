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
	// Reset clears unwritten indexer state.
	Reset()

	// AddUTXOs adds given slice of [outputUTXOs] to the indexer state
	AddUTXOs(outputUTXOs []*avax.UTXO)

	// AddUTXOsByID adds the given [inputUTXOIDs] to the indexer state.
	// The [getUTXOFn] function is used to get the underlying [avax.UTXO] from each
	// [avax.UTXOID] in the [inputUTXOIDs] slice.
	AddUTXOsByID(
		getUTXOF func(utxoID *avax.UTXOID) (*avax.UTXO, error),
		inputUTXOIDs []*avax.UTXOID,
	) error

	// Write persists the indexer state against the given [txID].
	// Reset() must be called manually to reset the state for next write.
	Write(txID ids.ID) error

	// Read returns list of txIDs indexed against the specified [address] and [assetID]
	// Returned slice length will be less than or equal to [pageSize].
	// [cursor] defines the offset to read the entries from
	Read(address ids.ShortID, assetID ids.ID, cursor, pageSize uint64) ([]ids.ID, error)
}

var idxKey = []byte("idx")

// indexer implements AddressTxsIndexer
type indexer struct {
	// Address -> AssetID --> Present if the address's balance
	// of the asset has changed since last Write
	addressAssetIDTxMap map[ids.ShortID]map[ids.ID]struct{}
	db                  *versiondb.Database
	log                 logging.Logger
	metrics             Metrics
}

// addTransferOutput indexes given assetID and any number of addresses linked to the transferOutput
// to the provided vm.addressAssetIDIndex
func (i *indexer) addTransferOutput(assetID ids.ID, addrs []ids.ShortID) {
	for _, address := range addrs {
		if _, exists := i.addressAssetIDTxMap[address]; !exists {
			i.addressAssetIDTxMap[address] = make(map[ids.ID]struct{})
		}
		i.addressAssetIDTxMap[address][assetID] = struct{}{}
	}
}

// AddUTXOsByID adds given the given UTXOs to the indexer's unwritten state.
// [getUTXOF] is used to look up UTXOs by their ID.
func (i *indexer) AddUTXOsByID(
	getUTXOF func(utxoid *avax.UTXOID) (*avax.UTXO, error),
	inputUTXOs []*avax.UTXOID,
) error {
	for _, utxoID := range inputUTXOs {
		utxo, err := getUTXOF(utxoID)
		if err != nil {
			return err // should never happen
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.log.Verbo("skipping utxo %s for indexing because it isn't a secp256k1fx.TransferOutput", utxo.InputID())
			continue
		}

		i.addTransferOutput(utxo.AssetID(), out.Addrs)
	}
	return nil
}

// AddUTXOs adds given [utxos] to the indexer
func (i *indexer) AddUTXOs(utxos []*avax.UTXO) {
	for _, utxo := range utxos {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.log.Verbo("Skipping output utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID())
			continue
		}

		i.addTransferOutput(utxo.AssetID(), out.Addrs)
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
func (i *indexer) Write(txID ids.ID) error {
	for address, assetIDs := range i.addressAssetIDTxMap {
		addressPrefixDB := prefixdb.New(address[:], i.db)
		for assetID := range assetIDs {
			assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)

			var idx uint64
			idxBytes, err := assetPrefixDB.Get(idxKey)
			switch {
			case err != nil && err != database.ErrNotFound:
				// Unexpected error
				i.log.Fatal("Error checking idx value exists when writing txID [%s], address [%s]: %s", txID, address, err)
				return err
			case err == database.ErrNotFound:
				// idx not found; this must be the first entry.
				idx = 0
				idxBytes = make([]byte, wrappers.LongLen)
				binary.BigEndian.PutUint64(idxBytes, idx)
			default:
				// Parse [idxBytes]
				idx = binary.BigEndian.Uint64(idxBytes)
				i.log.Verbo("fetched index %d", idx)
			}

			i.log.Debug("Writing at index %d txID %s", idx, txID)
			if err := assetPrefixDB.Put(idxBytes, txID[:]); err != nil {
				i.log.Fatal("Failed to save txID [%s], idx [%d] to the address [%s], assetID [%s] prefix DB %s", txID, idx, address, assetID, err)
				return err
			}

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				i.log.Fatal("Failed to update index for txID [%s] to the address [%s], assetID [%s] prefix DB: %s", txID, address, assetID, err)
				return err
			}
		}
		delete(i.addressAssetIDTxMap, address)
	}
	i.metrics.numTxsIndexed.Observe(1)
	return nil
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

func (i *indexer) Reset() {
	i.addressAssetIDTxMap = make(map[ids.ShortID]map[ids.ID]struct{})
}

// Returns a new AddressTxsIndexer.
// The returned indexer ignores UTXOs that are not type secp256k1fx.TransferOutput.
func NewAddressTxsIndexer(db *versiondb.Database, log logging.Logger, m Metrics) AddressTxsIndexer {
	return &indexer{
		addressAssetIDTxMap: make(map[ids.ShortID]map[ids.ID]struct{}),
		db:                  db,
		log:                 log,
		metrics:             m,
	}
}

type noIndexer struct{}

func NewNoIndexer() AddressTxsIndexer {
	return &noIndexer{}
}

func (i *noIndexer) AddUTXOsByID(func(utxoid *avax.UTXOID) (*avax.UTXO, error), []*avax.UTXOID) error {
	return nil
}

func (i *noIndexer) AddTransferOutput(ids.ID, *secp256k1fx.TransferOutput) {}

func (i *noIndexer) AddUTXOs([]*avax.UTXO) {}

func (i *noIndexer) Write(ids.ID) error {
	return nil
}

func (i *noIndexer) Reset() {}

func (i *noIndexer) Read(ids.ShortID, ids.ID, uint64, uint64) ([]ids.ID, error) {
	return nil, nil
}
