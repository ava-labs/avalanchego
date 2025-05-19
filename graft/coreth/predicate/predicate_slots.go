// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

type PredicaterExistChecker interface {
	PredicaterExists(common.Address) bool
}

// PreparePredicateStorageSlots populates the the predicate storage slots of a transaction's access list
// Note: if an address is specified multiple times in the access list, each storage slot for that address is
// appended to a slice of byte slices. Each byte slice represents a predicate, making it a slice of predicates
// for each access list address, and every predicate in the slice goes through verification.
func PreparePredicateStorageSlots(rules PredicaterExistChecker, list types.AccessList) map[common.Address][][]byte {
	predicateStorageSlots := make(map[common.Address][][]byte)
	for _, el := range list {
		if !rules.PredicaterExists(el.Address) {
			continue
		}
		predicateStorageSlots[el.Address] = append(predicateStorageSlots[el.Address], utils.HashSliceToBytes(el.StorageKeys))
	}

	return predicateStorageSlots
}
