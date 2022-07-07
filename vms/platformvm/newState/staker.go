// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type Staker struct {
	TxID            ids.ID
	NodeID          ids.NodeID
	SubnetID        ids.ID
	Weight          uint64
	StartTime       time.Time
	EndTime         time.Time
	PotentialReward uint64
	Priority        Priority
}

type Priority struct {
	Current byte
	Pending byte
}
