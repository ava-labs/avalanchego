// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// GetBalance returns the current balance of [addrs]
func GetBalance(db UTXOReader, addrs set.Set[ids.ShortID]) (uint64, error) {
	utxos, err := GetAllUTXOs(db, addrs)
	if err != nil {
		return 0, fmt.Errorf("couldn't get UTXOs: %w", err)
	}
	balance := uint64(0)
	for _, utxo := range utxos {
		if out, ok := utxo.Out.(Amounter); ok {
			balance, err = safemath.Add(out.Amount(), balance)
			if err != nil {
				return 0, err
			}
		}
	}
	return balance, nil
}

func GetAllUTXOs(db UTXOReader, addrs set.Set[ids.ShortID]) ([]*UTXO, error) {
	utxos, _, _, err := GetPaginatedUTXOs(
		db,
		addrs,
		ids.ShortEmpty,
		ids.Empty,
		math.MaxInt,
	)
	return utxos, err
}

func GetNextOutputIndex(utxos UTXOGetter, txID ids.ID) (uint32, error) {
	for i := uint32(0); i < math.MaxUint32; i++ {
		utxoID := UTXOID{
			TxID:        txID,
			OutputIndex: i,
		}

		_, err := utxos.GetUTXO(utxoID.InputID())
		switch {
		case errors.Is(err, database.ErrNotFound):
			return i, nil
		case err != nil:
			return 0, err
		}
	}

	panic("output index out of range")
}

// GetPaginatedUTXOs returns UTXOs such that at least one of the addresses in
// [addrs] is referenced.
//
// Returns at most [limit] UTXOs.
//
// Only returns UTXOs associated with addresses >= [startAddr].
//
// For address [startAddr], only returns UTXOs whose IDs are greater than
// [startUTXOID].
//
// Returns:
// * The fetched UTXOs
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
func GetPaginatedUTXOs(
	db UTXOReader,
	addrs set.Set[ids.ShortID],
	lastAddr ids.ShortID,
	lastUTXOID ids.ID,
	limit int,
) ([]*UTXO, ids.ShortID, ids.ID, error) {
	var (
		utxos      []*UTXO
		seen       set.Set[ids.ID] // IDs of UTXOs already in the list
		searchSize = limit         // the limit diminishes which can impact the expected return
		addrsList  = addrs.List()
	)
	utils.Sort(addrsList) // enforces the same ordering for pagination
	for _, addr := range addrsList {
		start := ids.Empty
		if comp := bytes.Compare(addr.Bytes(), lastAddr.Bytes()); comp == -1 { // Skip addresses before [startAddr]
			continue
		} else if comp == 0 {
			start = lastUTXOID
		}

		lastAddr = addr // The last address searched

		utxoIDs, err := db.UTXOIDs(addr.Bytes(), start, searchSize) // Get UTXOs associated with [addr]
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("couldn't get UTXOs for address %s: %w", addr, err)
		}
		for _, utxoID := range utxoIDs {
			lastUTXOID = utxoID // The last searched UTXO - not the last found

			if seen.Contains(utxoID) { // Already have this UTXO in the list
				continue
			}

			utxo, err := db.GetUTXO(utxoID)
			if err != nil {
				return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("couldn't get UTXO %s: %w", utxoID, err)
			}

			utxos = append(utxos, utxo)
			seen.Add(utxoID)
			limit--
			if limit <= 0 {
				return utxos, lastAddr, lastUTXOID, nil // Found [limit] utxos; stop.
			}
		}
	}
	return utxos, lastAddr, lastUTXOID, nil // Didn't reach the [limit] utxos; no more were found
}
