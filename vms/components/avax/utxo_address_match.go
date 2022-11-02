// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import reflect "reflect"

func (utxo *UTXO) MatchAddresses(addr []byte) bool {
	if addresses, ok := utxo.Out.(Addressable); ok {
		for _, address := range addresses.Addresses() {
			if reflect.DeepEqual(address, addr) {
				return true
			}
		}
	}
	return false
}
