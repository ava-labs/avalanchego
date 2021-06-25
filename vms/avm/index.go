// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var IndexingEnabled = false

// CommitIndex commits given addresses map to the database. The database structure is thus:
// [address]
// |  [assetID]
// |  |
// |  | "idx" => 2 		Running transaction index key, represents the next index
// |  | "0"   => txID1
// |  | "1"   => txID1
func CommitIndex(addresses map[ids.ShortID]map[ids.ID]struct{}, tx *UniqueTx) error {
	if !IndexingEnabled {
		return nil
	}

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
	if !IndexingEnabled {
		return
	}

	for _, address := range transferOutput.Addrs {
		if _, exists := vm.addressAssetIDIndex[address]; !exists {
			vm.addressAssetIDIndex[address] = make(map[ids.ID]struct{})
		}
		vm.addressAssetIDIndex[address][assetID] = struct{}{}
	}
}

func IndexInputUTXOs(vm *VM, inputUTXOs []*avax.UTXOID) error {
	if !IndexingEnabled {
		return nil
	}

	for _, utxoID := range inputUTXOs {
		// maybe this won't work and we'll need to get it from shared memory
		// todo fix using shared memory?
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
			vm.ctx.Log.Debug("Skipping input utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		IndexTransferOutput(vm, utxo.AssetID(), out)
	}
	return nil
}

func IndexOutputUTXOs(vm *VM, outputUTXOs []*avax.UTXO) {
	if !IndexingEnabled {
		return
	}

	for _, utxo := range outputUTXOs {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			vm.ctx.Log.Debug("Skipping output utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		IndexTransferOutput(vm, utxo.AssetID(), out)
	}
}

func ResetIndexMap(vm *VM) {
	vm.addressAssetIDIndex = make(map[ids.ShortID]map[ids.ID]struct{})
}
