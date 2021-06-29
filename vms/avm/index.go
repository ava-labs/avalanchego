// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Maintains an index of an address --> IDs of transactions that changed that address's balance.
// This includes both transactions that increased the address's balance and those that decreased it.
// An address's balance is said to have changed by a transaction if either:
// 1) An input UTXO to the transaction was at least partially owned by the address
// 2) An output of the transaction is at least partially owned by the address.
type AddressTxsIndexer interface {
	AddUTXOs(outputUTXOs []*avax.UTXO)
	AddUTXOIDs(vm *VM, inputUTXOs []*avax.UTXOID) error
	CommitIndex(txID ids.ID) error
	Reset()
}

// indexer implements AddressTxsIndexer
type indexer struct {
	// Maps address -> []AssetID Set
	addressAssetIDTxMap map[ids.ShortID]map[ids.ID]struct{}
	db                  *versiondb.Database
	log                 logging.Logger
	metrics             metrics
}

// AddTransferOutput indexes given assetID and any number of addresses linked to the transferOutput
// to the provided vm.addressAssetIDIndex
func (i *indexer) addTransferOutput(assetID ids.ID, transferOutput *secp256k1fx.TransferOutput) {
	for _, address := range transferOutput.Addrs {
		if _, exists := i.addressAssetIDTxMap[address]; !exists {
			i.addressAssetIDTxMap[address] = make(map[ids.ID]struct{})
		}
		i.addressAssetIDTxMap[address][assetID] = struct{}{}
	}
}

func (i *indexer) AddUTXOIDs(vm *VM, inputUTXOs []*avax.UTXOID) error {
	for _, utxoID := range inputUTXOs {
		utxo, err := vm.getUTXO(utxoID)
		if err != nil {
			// should never happen
			return err
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.log.Verbo("Skipping input utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		i.addTransferOutput(utxo.AssetID(), out)
	}
	return nil
}

func (i *indexer) AddUTXOs(outputUTXOs []*avax.UTXO) {
	for _, utxo := range outputUTXOs {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.log.Verbo("Skipping output utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		i.addTransferOutput(utxo.AssetID(), out)
	}
}

// CommitIndex commits given txID and already indexed data to the database.
// The database structure is thus:
// [address]
// |  [assetID]
// |  |
// |  | "idx" => 2 		Running transaction index key, represents the next index
// |  | "0"   => txID1
// |  | "1"   => txID1
func (i *indexer) CommitIndex(txID ids.ID) error {
	for address, assetIDMap := range i.addressAssetIDTxMap {
		addressPrefixDB := prefixdb.New(address[:], i.db)
		for assetID := range assetIDMap {
			assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)

			var idx uint64
			idxBytes, err := assetPrefixDB.Get(idxKey)
			switch {
			case err != nil && err != database.ErrNotFound:
				// Unexpected error
				i.log.Fatal("Error checking idx value exists: %s", err)
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
				i.log.Fatal("Failed to save transaction to the address, assetID prefix DB %s", err)
				return err
			}

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				i.log.Fatal("Failed to save transaction index to the address, assetID prefix DB: %s", err)
				return err
			}
		}
	}
	i.metrics.numTxsIndexed.Observe(1)
	return nil
}

func (i *indexer) Reset() {
	i.addressAssetIDTxMap = make(map[ids.ShortID]map[ids.ID]struct{})
}

func NewAddressTxsIndexer(db *versiondb.Database, log logging.Logger, metrics metrics) AddressTxsIndexer {
	return &indexer{
		addressAssetIDTxMap: make(map[ids.ShortID]map[ids.ID]struct{}),
		db:                  db,
		log:                 log,
		metrics:             metrics,
	}
}

type noIndexer struct{}

func NewNoIndexer() AddressTxsIndexer {
	return &noIndexer{}
}

func (i *noIndexer) AddUTXOIDs(*VM, []*avax.UTXOID) error {
	return nil
}

func (i *noIndexer) AddTransferOutput(ids.ID, *secp256k1fx.TransferOutput) {}

func (i *noIndexer) AddUTXOs([]*avax.UTXO) {}

func (i *noIndexer) CommitIndex(ids.ID) error {
	return nil
}

func (i *noIndexer) Reset() {}
