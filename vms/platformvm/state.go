// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

/*
// getUTXO returns the UTXO with the specified ID
func (vm *VM) getUTXO(db database.Database, id ids.ID) (*avax.UTXO, error) {
	utxoIntf, err := vm.State.Get(db, utxoTypeID, id)
	if err != nil {
		return nil, err
	}
	utxo, ok := utxoIntf.(*avax.UTXO)
	if !ok {
		err := fmt.Errorf("expected UTXO from database but got %T", utxoIntf)
		vm.Ctx.Log.Error(err.Error())
		return nil, err
	}
	return utxo, nil
}

// putUTXO persists the given UTXO
func (vm *VM) putUTXO(db database.Database, utxo *avax.UTXO) error {
	utxoID := utxo.InputID()
	if err := vm.State.Put(db, utxoTypeID, utxoID, utxo); err != nil {
		return err
	}

	// If this output lists addresses that it references index it
	if addressable, ok := utxo.Out.(avax.Addressable); ok {
		// For each owner of this UTXO, add to list of UTXOs owned by that addr
		for _, addrBytes := range addressable.Addresses() {
			if err := vm.putReferencingUTXO(db, addrBytes, utxoID); err != nil {
				// We assume that the maximum size of a byte slice that
				// can be stringified is at least the length of an address.
				// If conversion of address to string fails, ignore the error
				addrStr, _ := formatting.Encode(formatting.CB58, addrBytes)
				return fmt.Errorf("couldn't update UTXO set of address %s", addrStr)
			}
		}
	}
	return nil
}

// removeUTXO removes the UTXO with the given ID
// If the utxo doesn't exist, returns nil
func (vm *VM) removeUTXO(db database.Database, utxoID ids.ID) error {
	utxo, err := vm.getUTXO(db, utxoID) // Get the UTXO
	if err != nil {
		return nil
	}
	if err := vm.State.Put(db, utxoTypeID, utxoID, nil); err != nil { // remove the UTXO
		return err
	}
	// If this output lists addresses that it references remove the indices
	if addressable, ok := utxo.Out.(avax.Addressable); ok {
		// For each owner of this UTXO, remove from their list of UTXOs
		for _, addrBytes := range addressable.Addresses() {
			if err := vm.removeReferencingUTXO(db, addrBytes, utxoID); err != nil {
				// We assume that the maximum size of a byte slice that
				// can be stringified is at least the length of an address.
				// If conversion of address to string fails, ignore the error
				addrStr, _ := formatting.Encode(formatting.CB58, addrBytes)
				return fmt.Errorf("couldn't update UTXO set of address %s", addrStr)
			}
		}
	}
	return nil
}

// Return the IDs of UTXOs that reference [addr].
// Only returns UTXOs after [start].
// Returns at most [limit] UTXO IDs.
// Returns nil if no UTXOs reference [addr].

func (vm *VM) getReferencingUTXOs(db database.Database, addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	idSlice := []ids.ID(nil)

	iter := prefixdb.NewNested(addr, db).NewIteratorWithStart(start[:])
	defer iter.Release()
	numFetched := 0
	for numFetched < limit && iter.Next() {
		if keyID, err := ids.ToID(iter.Key()); err != nil {
			return nil, err
		} else if keyID != start {
			idSlice = append(idSlice, keyID)
			numFetched++

		}
	}
	return idSlice, nil
}

// Persist that the UTXO with ID [utxoID] references [addr]
func (vm *VM) putReferencingUTXO(db database.Database, addrBytes []byte, utxoID ids.ID) error {
	prefixedDB := prefixdb.NewNested(addrBytes, db)
	errs := wrappers.Errs{}
	errs.Add(
		prefixedDB.Put(utxoID[:], nil),
		prefixedDB.Close(),
	)
	return errs.Err
}

// Remove the UTXO with ID [utxoID] from the set of UTXOs that reference [addr]
func (vm *VM) removeReferencingUTXO(db database.Database, addrBytes []byte, utxoID ids.ID) error {
	prefixedDB := prefixdb.NewNested(addrBytes, db)
	return prefixedDB.Delete(utxoID[:])
}

// GetUTXOs returns UTXOs such that at least one of the addresses in [addrs] is referenced.
// Returns at most [limit] UTXOs.
// If [limit] <= 0 or [limit] > maxUTXOsToFetch, it is set to [maxUTXOsToFetch].
// Only returns UTXOs associated with addresses >= [startAddr].
// For address [startAddr], only returns UTXOs whose IDs are greater than [startUTXOID].
// Given a ![paginate] input all utxos will be fetched
// Returns:
// * The fetched UTXOs
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
func (vm *VM) GetUTXOs(
	db database.Database,
	addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
	paginate bool,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	if paginate {
		return vm.getPaginatedUTXOs(db, addrs, startAddr, startUTXOID, limit)
	}
	return vm.getAllUTXOs(db, addrs)
}

func (vm *VM) getPaginatedUTXOs(
	db database.Database,
	addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
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

		utxoIDs, err := vm.getReferencingUTXOs(db, addr.Bytes(), start, searchSize) // Get UTXOs associated with [addr]
		if err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXOs for address %s: %w", addr, err)
		}
		for _, utxoID := range utxoIDs {
			lastIndex = utxoID // The last searched UTXO - not the last found
			lastAddr = addr    // The last address searched that has UTXOs (even duplicated) - not the last found

			if seen.Contains(utxoID) { // Already have this UTXO in the list
				continue
			}

			utxo, err := vm.getUTXO(db, utxoID)
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
	db database.Database,
	addrs ids.ShortSet,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	var err error
	lastAddr := ids.ShortEmpty
	lastIndex := ids.Empty
	seen := make(ids.Set, maxUTXOsToFetch) // IDs of UTXOs already in the list
	utxos := make([]*avax.UTXO, 0, maxUTXOsToFetch)

	// enforces the same ordering for pagination
	addrsList := addrs.List()
	ids.SortShortIDs(addrsList)

	// iterate over the addresses and get all the utxos
	for _, addr := range addrsList {
		lastIndex, err = vm.getAllUniqueAddressUTXOs(db, addr, &seen, &utxos)
		if err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXOs for address %s: %w", addr, err)
		}

		if lastIndex != ids.Empty {
			lastAddr = addr // The last address searched that has UTXOs (even duplicated) - not the last found
		}
	}
	return utxos, lastAddr, lastIndex, nil
}

func (vm *VM) getAllUniqueAddressUTXOs(db database.Database, addr ids.ShortID, seen *ids.Set, utxos *[]*avax.UTXO) (ids.ID, error) {
	lastIndex := ids.Empty

	for {
		utxoIDs, err := vm.getReferencingUTXOs(db, addr.Bytes(), lastIndex, maxUTXOsToFetch) // Get UTXOs associated with [addr]
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

			utxo, err := vm.getUTXO(db, utxoID)
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
	utxos, _, _, err := vm.GetUTXOs(vm.internalState, addrs, ids.ShortEmpty, ids.Empty, -1, false)
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
*/
