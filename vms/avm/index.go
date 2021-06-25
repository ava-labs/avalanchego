// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// todo get rid
// getAddresses returns addresses mapped to assetID Set for a given transaction object
// map of address => [AssetIDs...]
func getAddresses(tx *UniqueTx, log logging.Logger) (map[ids.ShortID]map[ids.ID]struct{}, error) {
	addresses := map[ids.ShortID]map[ids.ID]struct{}{}

	for _, utxo := range tx.UTXOs() {
		log.Info("Transaction %s output utxo %s", tx.ID().String(), utxo.InputID().String())
	}

	for _, inUTXO := range tx.InputUTXOs() {
		log.Info("Transaction %s input utxo %s", tx.ID().String(), inUTXO.InputID().String())
	}

	// index input UTXOs
	for _, inputUTXO := range tx.InputUTXOs() {
		// todo this looks like it is using wrong reference
		utxo, err := tx.vm.getUTXO(inputUTXO) // gets cached utxo
		if err != nil {
			utxoID := inputUTXO.InputID()
			log.Error("Error getting UTXO %s for transaction %s: %s", utxoID.String(), tx.ID().String(), err)
			return nil, err
		}

		in, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}

		mapUTXOToAddressAndAsset(utxo.AssetID(), in, addresses)
	}

	// index output utxos
	for _, utxo := range tx.UTXOs() {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}

		mapUTXOToAddressAndAsset(utxo.AssetID(), out, addresses)
	}

	return addresses, nil
}

// todo get rid
// mapUTXOToAddressAndAsset maps a given secp256k1fx.TransferOutput's owner addresses to the specified assetID set
func mapUTXOToAddressAndAsset(assetID ids.ID, out *secp256k1fx.TransferOutput, addresses map[ids.ShortID]map[ids.ID]struct{}) {
	// For each address that exists, we add it to the map, adding the
	// assetID against it
	for _, addr := range out.OutputOwners.Addrs {
		if _, exists := addresses[addr]; !exists {
			addresses[addr] = make(map[ids.ID]struct{})
		}
		addresses[addr][assetID] = struct{}{}
	}
}

// todo get rid
// IndexTransaction Get transaction address and assetID map and proceed with indexing the transaction
// The transaction is indexed against the address -> assetID prefixdb database. Since we need to maintain
// the order of the transactions, the indexing is done as follows:
// [address]
// |  [assetID]
// |  |
// |  | "idx" => 3 		Running transaction index key, represents the next index
// |  | "1"   => txID1
// |  | "2"   => txID1
func IndexTransaction(tx *UniqueTx) error {
	txID := tx.ID()
	tx.vm.ctx.Log.Debug("Indexing transaction %s", txID.String())
	addresses, err := getAddresses(tx, tx.vm.ctx.Log)
	if err != nil {
		tx.vm.ctx.Log.Debug("Error while indexing transaction %s: %s", txID.String(), err)
		return err
	}

	tx.vm.ctx.Log.Debug("Retrieved address data for transaction %s, %s", txID.String(), addresses)
	for address, assetIDMap := range addresses {
		addressPrefixDB := prefixdb.New(address[:], tx.vm.db)
		for assetID := range assetIDMap {
			assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)

			var idx uint64
			idxBytes, err := assetPrefixDB.Get(idxKey)
			switch {
			case err != nil && err != database.ErrNotFound:
				// Unexpected error
				tx.vm.ctx.Log.Fatal("Error checking idx value exists: %s", err)
				return err
			case err == database.ErrNotFound:
				// idx not found; this must be the first entry.
				idx = 0
				idxBytes = make([]byte, wrappers.LongLen)
				binary.BigEndian.PutUint64(idxBytes, idx)
			default:
				// Parse [idxBytes]
				idx = binary.BigEndian.Uint64(idxBytes)
				tx.vm.ctx.Log.Debug("fetched index %d", idx)
			}

			tx.vm.ctx.Log.Debug("Writing at index %d txID %s", idx, txID)
			if err := assetPrefixDB.Put(idxBytes, txID[:]); err != nil {
				tx.vm.ctx.Log.Fatal("Failed to save transaction to the address, assetID prefix DB %s", err)
				return err
			}

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)
			tx.vm.ctx.Log.Debug("New index %d", idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				tx.vm.ctx.Log.Fatal("Failed to save transaction index to the address, assetID prefix DB: %s", err)
				return err
			}
		}
	}

	tx.vm.ctx.Log.Debug("Finished indexing transaction %s: err?:%s", txID.String(), err)
	return err
}

// CommitIndex commits given addresses map to the database. The database structure is thus:
// [address]
// |  [assetID]
// |  |
// |  | "idx" => 2 		Running transaction index key, represents the next index
// |  | "0"   => txID1
// |  | "1"   => txID1
func CommitIndex(addresses map[ids.ShortID]map[ids.ID]struct{}, tx *UniqueTx) error {
	txID := tx.ID()
	tx.vm.ctx.Log.Debug("Retrieved address data for transaction %s, %s", txID.String(), addresses)
	for address, assetIDMap := range addresses {
		addressPrefixDB := prefixdb.New(address[:], tx.vm.db)
		for assetID := range assetIDMap {
			assetPrefixDB := prefixdb.New(assetID[:], addressPrefixDB)

			var idx uint64
			idxBytes, err := assetPrefixDB.Get(idxKey)
			switch {
			case err != nil && err != database.ErrNotFound:
				// Unexpected error
				tx.vm.ctx.Log.Fatal("Error checking idx value exists: %s", err)
				return err
			case err == database.ErrNotFound:
				// idx not found; this must be the first entry.
				idx = 0
				idxBytes = make([]byte, wrappers.LongLen)
				binary.BigEndian.PutUint64(idxBytes, idx)
			default:
				// Parse [idxBytes]
				idx = binary.BigEndian.Uint64(idxBytes)
				tx.vm.ctx.Log.Debug("fetched index %d", idx)
			}

			tx.vm.ctx.Log.Debug("Writing at index %d txID %s", idx, txID)
			if err := assetPrefixDB.Put(idxBytes, txID[:]); err != nil {
				tx.vm.ctx.Log.Fatal("Failed to save transaction to the address, assetID prefix DB %s", err)
				return err
			}

			// increment and store the index for next use
			idx++
			binary.BigEndian.PutUint64(idxBytes, idx)
			tx.vm.ctx.Log.Debug("New index %d", idx)

			if err := assetPrefixDB.Put(idxKey, idxBytes); err != nil {
				tx.vm.ctx.Log.Fatal("Failed to save transaction index to the address, assetID prefix DB: %s", err)
				return err
			}
		}
	}
	return nil
}

// IndexTransferOutput indexes given assetID and any number of addresses linked to the transferOutput
// to the provided vm.addressAssetIDIndex
// todo no need to pass whole VM, pass in just the map
func IndexTransferOutput(vm *VM, assetID ids.ID, transferOutput *secp256k1fx.TransferOutput) {
	for _, address := range transferOutput.Addrs {
		if _, exists := vm.addressAssetIDIndex[address]; !exists {
			vm.addressAssetIDIndex[address] = make(map[ids.ID]struct{})
		}
		vm.addressAssetIDIndex[address][assetID] = struct{}{}
	}
}

func IndexInputUTXOs(vm *VM, inputUTXOs []*avax.UTXOID) {
	for _, utxoID := range inputUTXOs {
		// maybe this won't work and we'll need to get it from shared memory
		// todo fix using shared memory?
		utxo, err := vm.getUTXO(utxoID)
		if err != nil {
			vm.ctx.Log.Error("Error fetching input utxo %s, err: %s", utxoID.InputID().String(), err)
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			vm.ctx.Log.Debug("Skipping input utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		IndexTransferOutput(vm, utxo.AssetID(), out)
	}
}

func IndexOutputUTXOs(vm *VM, outputUTXOs []*avax.UTXO) {
	for _, utxo := range outputUTXOs {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			vm.ctx.Log.Debug("Skipping output utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		IndexTransferOutput(vm, utxo.AssetID(), out)
	}
}
