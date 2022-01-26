// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
)

var errDuplicatedContainerID = errors.New("inbound message contains duplicated container ID")

func getContainerIDs(msg message.InboundMessage) ([]ids.ID, error) {
	containerIDsBytes := msg.Get(message.ContainerIDs).([][]byte)
	res := make([]ids.ID, len(containerIDsBytes))
	idSet := ids.NewSet(len(containerIDsBytes))
	for i, containerIDBytes := range containerIDsBytes {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			return nil, err
		}
		if idSet.Contains(containerID) {
			return nil, errDuplicatedContainerID
		}
		res[i] = containerID
		idSet.Add(containerID)
	}
	return res, nil
}
