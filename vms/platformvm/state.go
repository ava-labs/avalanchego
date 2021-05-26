// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// getPaginatedUTXOs returns UTXOs such that at least one of the addresses in
// [addrs] is referenced.
// Returns at most [limit] UTXOs.
// If [limit] <= 0 or [limit] > maxUTXOsToFetch, it is set to [maxUTXOsToFetch].
// Only returns UTXOs associated with addresses >= [startAddr].
// For address [startAddr], only returns UTXOs whose IDs are greater than
// [startUTXOID].
// Returns:
// * The fetched UTXOs
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
func (vm *VM) getPaginatedUTXOs(
	addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	lastAddr := ids.ShortEmpty
	lastIndex := ids.Empty

	utxos := make([]*avax.UTXO, 0, limit)
	seen := make(ids.Set, limit) // IDs of UTXOs already in the list
	searchSize := limit          // the limit diminishes which can impact the expected return

	// enforces the same ordering for pagination
	addrsList := addrs.List()
	ids.SortShortIDs(addrsList)

	for _, addr := range addrsList {
		start := ids.Empty
		if comp := bytes.Compare(addr.Bytes(), startAddr.Bytes()); comp == -1 { // Skip addresses before [startAddr]
			continue
		} else if comp == 0 {
			start = startUTXOID
		}

		utxoIDs, err := vm.internalState.UTXOIDs(addr.Bytes(), start, searchSize) // Get UTXOs associated with [addr]
		if err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXOs for address %s: %w", addr, err)
		}
		for _, utxoID := range utxoIDs {
			lastIndex = utxoID // The last searched UTXO - not the last found
			lastAddr = addr    // The last address searched that has UTXOs (even duplicated) - not the last found

			if seen.Contains(utxoID) { // Already have this UTXO in the list
				continue
			}

			utxo, err := vm.internalState.GetUTXO(utxoID)
			if err != nil {
				return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXO %s: %w", utxoID, err)
			}

			utxos = append(utxos, utxo)
			seen.Add(utxoID)
			limit--
			if limit <= 0 {
				return utxos, lastAddr, lastIndex, nil // Found [limit] utxos; stop.
			}
		}
	}
	return utxos, lastAddr, lastIndex, nil // Didnt reach the [limit] utxos; no more were found
}

func (vm *VM) getAllUTXOs(
	addrs ids.ShortSet,
) ([]*avax.UTXO, error) {
	var err error
	seen := make(ids.Set, maxUTXOsToFetch) // IDs of UTXOs already in the list
	utxos := make([]*avax.UTXO, 0, maxUTXOsToFetch)

	// enforces the same ordering for pagination
	addrsList := addrs.List()
	ids.SortShortIDs(addrsList)

	// iterate over the addresses and get all the utxos
	for _, addr := range addrsList {
		_, err = vm.getAllUniqueAddressUTXOs(addr, &seen, &utxos)
		if err != nil {
			return nil, fmt.Errorf("couldn't get UTXOs for address %s: %w", addr, err)
		}
	}
	return utxos, nil
}

func (vm *VM) getAllUniqueAddressUTXOs(addr ids.ShortID, seen *ids.Set, utxos *[]*avax.UTXO) (ids.ID, error) {
	lastIndex := ids.Empty

	for {
		utxoIDs, err := vm.internalState.UTXOIDs(addr.Bytes(), lastIndex, maxUTXOsToFetch) // Get UTXOs associated with [addr]
		if err != nil {
			return ids.ID{}, err
		}

		if len(utxoIDs) == 0 {
			return lastIndex, nil
		}

		for _, utxoID := range utxoIDs {
			lastIndex = utxoID // The last searched UTXO - not the last found

			if seen.Contains(utxoID) { // Already have this UTXO in the list
				continue
			}

			utxo, err := vm.internalState.GetUTXO(utxoID)
			if err != nil {
				return ids.ID{}, err
			}
			*utxos = append(*utxos, utxo)
			seen.Add(utxoID)
		}
	}
}

// getBalance returns the current balance of [addrs]
func (vm *VM) getBalance(addrs ids.ShortSet) (uint64, error) {
	utxos, err := vm.getAllUTXOs(addrs)
	if err != nil {
		return 0, fmt.Errorf("couldn't get UTXOs: %w", err)
	}
	balance := uint64(0)
	for _, utxo := range utxos {
		if out, ok := utxo.Out.(avax.Amounter); ok {
			if balance, err = safemath.Add64(out.Amount(), balance); err != nil {
				return 0, err
			}
		}
	}
	return balance, nil
}
