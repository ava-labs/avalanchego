// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var errDuplicatedID = errors.New("inbound message contains duplicated ID")

func getIDs(idsBytes [][]byte) ([]ids.ID, error) {
	res := make([]ids.ID, len(idsBytes))
	idSet := ids.NewSet(len(idsBytes))
	for i, bytes := range idsBytes {
		id, err := ids.ToID(bytes)
		if err != nil {
			return nil, err
		}
		if idSet.Contains(id) {
			return nil, errDuplicatedID
		}
		res[i] = id
		idSet.Add(id)
	}
	return res, nil
}

// TODO: Enforce that the numbers are sorted to make this verification more
//       efficient.
func isUnique(nums []uint64) bool {
	numsSet := make(map[uint64]struct{}, len(nums))
	for _, num := range nums {
		if _, found := numsSet[num]; found {
			return false
		}
		numsSet[num] = struct{}{}
	}
	return true
}
