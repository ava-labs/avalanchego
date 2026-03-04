// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

var _ State = (*TestState)(nil)

type uptime struct {
	upDuration  time.Duration
	lastUpdated time.Time
	startTime   time.Time
}

type TestState struct {
	dbReadError  error
	dbWriteError error
	nodes        map[ids.NodeID]*uptime
}

func NewTestState() *TestState {
	return &TestState{
		nodes: make(map[ids.NodeID]*uptime),
	}
}

func (s *TestState) AddNode(nodeID ids.NodeID, startTime time.Time) {
	st := time.Unix(startTime.Unix(), 0)
	s.nodes[nodeID] = &uptime{
		lastUpdated: st,
		startTime:   st,
	}
}

func (s *TestState) GetUptime(nodeID ids.NodeID) (time.Duration, time.Time, error) {
	up, exists := s.nodes[nodeID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return up.upDuration, up.lastUpdated, s.dbReadError
}

func (s *TestState) SetUptime(nodeID ids.NodeID, upDuration time.Duration, lastUpdated time.Time) error {
	up, exists := s.nodes[nodeID]
	if !exists {
		return database.ErrNotFound
	}
	up.upDuration = upDuration
	up.lastUpdated = time.Unix(lastUpdated.Unix(), 0)
	return s.dbWriteError
}

func (s *TestState) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	up, exists := s.nodes[nodeID]
	if !exists {
		return time.Time{}, database.ErrNotFound
	}
	return up.startTime, s.dbReadError
}
