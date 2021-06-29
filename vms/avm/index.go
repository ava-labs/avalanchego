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

type AddressTxsIndexer interface {
	AddUTXOs(outputUTXOs []*avax.UTXO)
	AddUTXOIDs(vm *VM, inputUTXOs []*avax.UTXOID) error
	CommitIndex(txID ids.ID) error
	Reset()
}

// statefulAddressTxsIndexer indexes transactions for given an address and an assetID
// Assumes state lock is held when this is called
type statefulAddressTxsIndexer struct {
	// Maps address -> []AssetID Set
	addressAssetIDTxMap map[ids.ShortID]map[ids.ID]struct{}
	db                  *versiondb.Database
	logger              logging.Logger
	m                   metrics
}

// AddTransferOutput IndexTransferOutput indexes given assetID and any number of addresses linked to the transferOutput
// to the provided vm.addressAssetIDIndex
func (i *statefulAddressTxsIndexer) addTransferOutput(assetID ids.ID, transferOutput *secp256k1fx.TransferOutput) {
	for _, address := range transferOutput.Addrs {
		if _, exists := i.addressAssetIDTxMap[address]; !exists {
			i.addressAssetIDTxMap[address] = make(map[ids.ID]struct{})
		}
		i.addressAssetIDTxMap[address][assetID] = struct{}{}
	}
}

func (i *statefulAddressTxsIndexer) AddUTXOIDs(vm *VM, inputUTXOs []*avax.UTXOID) error {
	for _, utxoID := range inputUTXOs {
		utxo, err := vm.getUTXO(utxoID)
		if err != nil {
			// should never happen
			return err
		}

		// typically in case of missing utxo there is an err above
		// this is just a catch all for the edge case
		if utxo == nil {
			return errMissingUTXO
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.logger.Debug("Skipping input utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		i.addTransferOutput(utxo.AssetID(), out)
	}
	return nil
}

func (i *statefulAddressTxsIndexer) AddUTXOs(outputUTXOs []*avax.UTXO) {
	for _, utxo := range outputUTXOs {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			i.logger.Debug("Skipping output utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
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
func (i *statefulAddressTxsIndexer) CommitIndex(txID ids.ID) error {
	for address, assetIDMap := range i.addressAssetIDTxMap {
		addressPrefixDB := prefixdb.New(address[:], i.db)
		for assetID := range assetIDMap {
			assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)

			var idx uint64
			idxBytes, err := assetPrefixDB.Get(idxKey)
			switch {
			case err != nil && err != database.ErrNotFound:
				// Unexpected error
				i.logger.Fatal("Error checking idx value exists: %s", err)
				return err
			case err == database.ErrNotFound:
				// idx not found; this must be the first entry.
				idx = 0
				idxBytes = make([]byte, wrappers.LongLen)
				binary.BigEndian.PutUint64(idxBytes, idx)
			default:
				// Parse [idxBytes]
				idx = binary.BigEndian.Uint64(idxBytes)
				i.logger.Debug("fetched index %d", idx)
			}

			i.logger.Debug("Writing at index %d txID %s", idx, txID)
			if err := assetPrefixDB.Put(idxBytes, txID[:]); err != nil {
				i.logger.Fatal("Failed to save transaction to the address, assetID prefix DB %s", err)
				return err
			}

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)
			i.logger.Debug("New index %d", idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				i.logger.Fatal("Failed to save transaction index to the address, assetID prefix DB: %s", err)
				return err
			}
		}
	}
	i.m.numTxsIndexed.Observe(1)
	return nil
}

func (i *statefulAddressTxsIndexer) Reset() {
	i.addressAssetIDTxMap = make(map[ids.ShortID]map[ids.ID]struct{})
}

func NewAddressTxsIndexer(db *versiondb.Database, logger logging.Logger, m metrics) AddressTxsIndexer {
	return &statefulAddressTxsIndexer{
		addressAssetIDTxMap: make(map[ids.ShortID]map[ids.ID]struct{}),
		db:                  db,
		logger:              logger,
		m:                   m,
	}
}

type noOpAddressTxsIndexer struct{}

func NewNoOpAddressTxsIndexer() AddressTxsIndexer {
	return &noOpAddressTxsIndexer{}
}

func (i *noOpAddressTxsIndexer) AddUTXOIDs(vm *VM, inputUTXOs []*avax.UTXOID) error {
	return nil
}

func (i *noOpAddressTxsIndexer) AddTransferOutput(assetID ids.ID, transferOutput *secp256k1fx.TransferOutput) {
	// no op
}

func (i *noOpAddressTxsIndexer) AddUTXOs(outputUTXOs []*avax.UTXO) {
	// no op
}

func (i *noOpAddressTxsIndexer) CommitIndex(txID ids.ID) error {
	return nil
}

func (i *noOpAddressTxsIndexer) Reset() {
	// no op
}
