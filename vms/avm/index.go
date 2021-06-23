// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// getAddresses returns addresses mapped to assetID Set for a given transaction object
// map of address => [AssetIDs...]
func getAddresses(tx *UniqueTx) (map[ids.ShortID]map[ids.ID]struct{}, error) {
	addresses := map[ids.ShortID]map[ids.ID]struct{}{}

	// index input UTXOs
	for _, utxoID := range tx.InputUTXOs() {
		utxo, err := tx.vm.getUTXO(utxoID) // gets cached utxo
		if err != nil {
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
	addresses, err := getAddresses(tx)
	if err != nil {
		return err
	}

	tx.vm.ctx.Log.Debug("Retrieved address data %s", addresses)
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
	return err
}
