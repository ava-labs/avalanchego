// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/google/btree"
)

var _ btree.Item = &Staker{}

type StakerIterator interface {
	Next() bool
	Value() *Staker
	Release()
}

type Staker struct {
	TxID            ids.ID
	NodeID          ids.NodeID
	SubnetID        ids.ID
	Weight          uint64
	StartTime       time.Time
	EndTime         time.Time
	PotentialReward uint64

	// The following fields are only used for the staker tree.
	NextTime time.Time
	Priority byte
}

func (s *Staker) Less(thanIntf btree.Item) bool {
	than := thanIntf.(*Staker)

	if s.NextTime.Before(than.NextTime) {
		return true
	}
	if than.NextTime.Before(s.NextTime) {
		return false
	}

	if s.Priority > than.Priority {
		return true
	}
	if than.Priority > s.Priority {
		return false
	}

	return bytes.Compare(s.TxID[:], than.TxID[:]) == -1
}
