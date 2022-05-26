// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
)

func TestPeers(t *testing.T) {
	assert := assert.New(t)

	nodeID := ids.GenerateTestNodeID()

	p := NewPeers()

	assert.Zero(p.ConnectedWeight())
	assert.Empty(p.PreferredPeers())

	p.OnValidatorAdded(nodeID, 5)
	assert.Zero(p.ConnectedWeight())
	assert.Empty(p.PreferredPeers())

	err := p.Connected(nodeID, version.CurrentApp)
	assert.NoError(err)
	assert.EqualValues(5, p.ConnectedWeight())
	assert.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorWeightChanged(nodeID, 5, 10)
	assert.EqualValues(10, p.ConnectedWeight())
	assert.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorRemoved(nodeID, 10)
	assert.Zero(p.ConnectedWeight())
	assert.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorAdded(nodeID, 5)
	assert.EqualValues(5, p.ConnectedWeight())
	assert.Contains(p.PreferredPeers(), nodeID)

	err = p.Disconnected(nodeID)
	assert.NoError(err)
	assert.Zero(p.ConnectedWeight())
	assert.Empty(p.PreferredPeers())
}
