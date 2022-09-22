// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
)

var (
	errDuplicatedID     = errors.New("inbound message contains duplicated ID")
	errDuplicatedHeight = errors.New("inbound message contains duplicated height")
)

func getIDs(field message.Field, msg message.InboundMessage) ([]ids.ID, error) {
	idsBytesIntf, err := msg.Get(field)
	if err != nil {
		return nil, err
	}
	idsBytes := idsBytesIntf.([][]byte)

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

func getSummaryHeights(msg message.InboundMessage) ([]uint64, error) {
	heightsIntf, err := msg.Get(message.SummaryHeights)
	if err != nil {
		return nil, err
	}
	heights := heightsIntf.([]uint64)

	heightsSet := make(map[uint64]struct{}, len(heights))
	for _, height := range heights {
		if _, found := heightsSet[height]; found {
			return nil, errDuplicatedHeight
		}
		heightsSet[height] = struct{}{}
	}
	return heights, nil
}
