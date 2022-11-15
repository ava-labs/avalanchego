// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

// Set a new state assigned to the address id
func (cs *caminoState) SetAddressStates(address ids.ShortID, states uint64) {
	cs.modifiedAddressStates[address] = states
	cs.addressStateCache.Evict(address)
}

// Return the current state (if exists) for an address
func (cs *caminoState) GetAddressStates(address ids.ShortID) (uint64, error) {
	// Try to get from modified state
	item, ok := cs.modifiedAddressStates[address]
	// Try to get from cache
	if !ok {
		var itemIntf interface{}
		if itemIntf, ok = cs.addressStateCache.Get(address); ok {
			item = itemIntf.(uint64)
		}
	}
	// Finally get it from database
	if !ok {
		uintBytes, err := cs.addressStateDB.Get(address[:])
		switch err {
		case nil:
			item = binary.LittleEndian.Uint64(uintBytes)
		case database.ErrNotFound:
			item = 0
		default:
			return 0, err
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
			binary.LittleEndian.PutUint64(buf, val)
			if err := cs.addressStateDB.Put(key[:], buf); err != nil {
				return err
			}
		}
	}
	return nil
}
