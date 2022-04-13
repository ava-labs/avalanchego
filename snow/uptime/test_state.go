// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/ids"
)

var _ State = &TestState{}

type uptime struct {
	upDuration  time.Duration
	lastUpdated time.Time
	startTime   time.Time
}

type TestState struct {
	dbReadError  error
	dbWriteError error
	nodes        map[ids.ShortID]*uptime
}

func NewTestState() *TestState {
	return &TestState{
		nodes: make(map[ids.ShortID]*uptime),
	}
}

func (s *TestState) AddNode(nodeID ids.ShortID, startTime time.Time) {
	s.nodes[nodeID] = &uptime{
		lastUpdated: startTime,
		startTime:   startTime,
	}
}

func (s *TestState) GetUptime(nodeID ids.ShortID) (time.Duration, time.Time, error) {
	up, exists := s.nodes[nodeID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return up.upDuration, up.lastUpdated, s.dbReadError
}

func (s *TestState) SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error {
	up, exists := s.nodes[nodeID]
	if !exists {
		return database.ErrNotFound
	}
	up.upDuration = upDuration
	up.lastUpdated = lastUpdated
	return s.dbWriteError
}

func (s *TestState) GetStartTime(nodeID ids.ShortID) (time.Time, error) {
	up, exists := s.nodes[nodeID]
	if !exists {
		return time.Time{}, database.ErrNotFound
	}
	return up.startTime, s.dbReadError
}
