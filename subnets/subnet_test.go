// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestSingleChainSubnetFullySyncedWithStateSync(t *testing.T) {
	// State Sync  |-----X---------------------X-----|
	// Bootstrap   |---------X-----------X-----------|
	// NormalOps   |-------------------------------X-|
	// FullySynced |---------------------------X-----|

	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()

	tracker := New(nodeID, Config{})
	require.False(tracker.IsSubnetSynced())

	tracker.AddChain(chainID)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chainID, snow.StateSyncing)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chainID, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chainID, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chainID, snow.StateSyncing)
	require.True(tracker.IsSubnetSynced())

	tracker.StartState(chainID, snow.NormalOp)
	require.True(tracker.IsSubnetSynced())
}

func TestSingleChainSubnetFullySyncedWithoutStateSync(t *testing.T) {
	// State Sync  |---------------------------------|
	// Bootstrap   |---------X-----------X-----------|
	// NormalOps   |------------------------X--------|
	// FullySynced |---------------------X-----------|

	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()

	tracker := New(nodeID, Config{})
	require.False(tracker.IsSubnetSynced())

	tracker.AddChain(chainID)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chainID, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chainID, snow.Bootstrapping)
	require.True(tracker.IsSubnetSynced())

	tracker.StartState(chainID, snow.NormalOp)
	require.True(tracker.IsSubnetSynced())
}

func TestMultipleChainsSubnetNoRestart(t *testing.T) {
	// State Sync  |------------------Ch1-------------------Ch1--------------------|
	// Bootstrap   |--Ch0------Ch0-----Ch2-----Ch2------------Ch1--Ch1-------------|
	// NormalOps   |---------------------------------------------------Ch0-Ch2-Ch1-|
	// FullySynced |------------------------------------------------X--------------|

	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	chain0 := ids.GenerateTestID()
	chain1 := ids.GenerateTestID()
	chain2 := ids.GenerateTestID()

	tracker := New(nodeID, Config{})
	require.False(tracker.IsSubnetSynced())

	tracker.AddChain(chain0)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chain0, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.AddChain(chain1)
	require.False(tracker.IsSubnetSynced())

	tracker.AddChain(chain2)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain0, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chain1, snow.StateSyncing)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chain2, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain2, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain1, snow.StateSyncing)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chain1, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain1, snow.Bootstrapping)
	require.True(tracker.IsSubnetSynced())

	tracker.StartState(chain0, snow.NormalOp)
	require.True(tracker.IsSubnetSynced())

	tracker.StartState(chain2, snow.NormalOp)
	require.True(tracker.IsSubnetSynced())

	tracker.StartState(chain1, snow.NormalOp)
	require.True(tracker.IsSubnetSynced())
}

func TestMultipleChainsSubnetWithRestart(t *testing.T) {
	// State Sync  |------------------Ch1----Ch1-----------------------------|
	// Bootstrap   |--Ch0------Ch0-----Ch0--------Ch1-----Ch1--Ch0-----------|
	// Normal Ops  |------------------------------------------------Ch0--Ch1-|
	// FullySynced |--------------------------------------------X------------|

	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	chain0 := ids.GenerateTestID()
	chain1 := ids.GenerateTestID()

	tracker := New(nodeID, Config{})
	require.False(tracker.IsSubnetSynced())

	tracker.AddChain(chain0)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chain0, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.AddChain(chain1)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain0, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chain1, snow.StateSyncing)
	require.False(tracker.IsSubnetSynced())

	// chain0 restarts bootstrapping while chain1 state syncs
	// Assume chain0 will take longer than chain1 to complete
	// the second bootstrap run
	tracker.StartState(chain0, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain1, snow.StateSyncing)
	require.False(tracker.IsSubnetSynced())

	tracker.StartState(chain1, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain1, snow.Bootstrapping)
	require.False(tracker.IsSubnetSynced())

	tracker.StopState(chain0, snow.Bootstrapping)
	require.True(tracker.IsSubnetSynced())

	tracker.StartState(chain0, snow.NormalOp)
	require.True(tracker.IsSubnetSynced())

	tracker.StartState(chain1, snow.NormalOp)
	require.True(tracker.IsSubnetSynced())
}

func TestIsAllowed(t *testing.T) {
	require := require.New(t)

	myNodeID := ids.GenerateTestNodeID()
	// Test with no rules
	s := New(myNodeID, Config{})
	require.True(s.IsAllowed(ids.GenerateTestNodeID(), true), "Validator should be allowed with no rules")
	require.True(s.IsAllowed(ids.GenerateTestNodeID(), false), "Non-validator should be allowed with no rules")

	// Test with validator only rules
	s = New(myNodeID, Config{
		ValidatorOnly: true,
	})
	require.True(s.IsAllowed(ids.GenerateTestNodeID(), true), "Validator should be allowed with validator only rules")
	require.True(s.IsAllowed(myNodeID, false), "Self node should be allowed with validator only rules")
	require.False(s.IsAllowed(ids.GenerateTestNodeID(), false), "Non-validator should not be allowed with validator only rules")

	// Test with validator only rules and allowed nodes
	allowedNodeID := ids.GenerateTestNodeID()
	s = New(myNodeID, Config{
		ValidatorOnly: true,
		AllowedNodes: set.Set[ids.NodeID]{
			allowedNodeID: struct{}{},
		},
	})
	require.True(s.IsAllowed(allowedNodeID, true), "Validator should be allowed with validator only rules and allowed nodes")
	require.True(s.IsAllowed(myNodeID, false), "Self node should be allowed with validator only rules")
	require.False(s.IsAllowed(ids.GenerateTestNodeID(), false), "Non-validator should not be allowed with validator only rules and allowed nodes")
	require.True(s.IsAllowed(allowedNodeID, true), "Non-validator allowed node should be allowed with validator only rules and allowed nodes")
}
