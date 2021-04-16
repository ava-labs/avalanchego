// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

type uptime struct {
	upDuration  time.Duration
	lastUpdated time.Time
}

type testState struct {
	nodes map[ids.ShortID]*uptime
}

func NewTestState() *testState {
	return &testState{
		nodes: make(map[ids.ShortID]*uptime),
	}
}

func (s *testState) addNode(nodeID ids.ShortID, startTime time.Time) {
	s.nodes[nodeID] = &uptime{
		lastUpdated: startTime,
	}
}

func (s *testState) deleteNode(nodeID ids.ShortID) {
	delete(s.nodes, nodeID)
}

func (s *testState) GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	up, exists := s.nodes[nodeID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return up.upDuration, up.lastUpdated, nil
}

func (s *testState) SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error {
	up, exists := s.nodes[nodeID]
	if !exists {
		return database.ErrNotFound
	}
	up.upDuration = upDuration
	up.lastUpdated = lastUpdated
	return nil
}

func TestNewManager(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()

	s := NewTestState()
	up := NewManager(s)
}
