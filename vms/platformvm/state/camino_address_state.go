// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Set a new state assigned to the address id
func (cs *caminoState) SetAddressStates(address ids.ShortID, states txs.AddressState) {
	cs.modifiedAddressStates[address] = states
	cs.addressStateCache.Evict(address)
}

// Return the current state (if exists) for an address
func (cs *caminoState) GetAddressStates(address ids.ShortID) (txs.AddressState, error) {
	// Try to get from modified state
	item, ok := cs.modifiedAddressStates[address]
	// Try to get from cache
	if !ok {
		item, ok = cs.addressStateCache.Get(address)
	}
	// Finally get it from database
	if !ok {
		uintBytes, err := cs.addressStateDB.Get(address[:])
		switch err {
		case nil:
			item = txs.AddressState(binary.LittleEndian.Uint64(uintBytes))
		case database.ErrNotFound:
			item = txs.AddressStateEmpty
		default:
			return txs.AddressStateEmpty, err
		}
		cs.addressStateCache.Put(address, item)
	}
	return item, nil
}

func (cs *caminoState) writeAddressStates() error {
	for key, val := range cs.modifiedAddressStates {
		delete(cs.modifiedAddressStates, key)
		if val == 0 {
			if err := cs.addressStateDB.Delete(key[:]); err != nil {
				return err
			}
		} else {
			buf := make([]byte, 8)
			binary.LittleEndian.PutUint64(buf, uint64(val))
			if err := cs.addressStateDB.Put(key[:], buf); err != nil {
				return err
			}
		}
	}
	return nil
}
