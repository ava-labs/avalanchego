// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errDuplicatedID = errors.New("inbound message contains duplicated ID")

func getIDs(idsBytes [][]byte) (set.Set[ids.ID], error) {
	var res set.Set[ids.ID]
	for _, bytes := range idsBytes {
		id, err := ids.ToID(bytes)
		if err != nil {
			return nil, err
		}
		res.Add(id)
	}
	if res.Len() != len(idsBytes) {
		return nil, errDuplicatedID
	}
	return res, nil
}
