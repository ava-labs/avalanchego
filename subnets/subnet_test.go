// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestSubnet(t *testing.T) {
	require := require.New(t)

	myNodeID := ids.GenerateTestNodeID()
	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()
	chainID2 := ids.GenerateTestID()

	s := New(myNodeID, Config{})
	s.AddChain(chainID0)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID0)
	require.True(s.IsBootstrapped(), "A subnet with only bootstrapped chains should be considered bootstrapped")

	s.AddChain(chainID1)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.AddChain(chainID2)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID1)
	require.False(s.IsBootstrapped(), "A subnet with one chain in bootstrapping shouldn't be considered bootstrapped")

	s.Bootstrapped(chainID2)
	require.True(s.IsBootstrapped(), "A subnet with only bootstrapped chains should be considered bootstrapped")
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
		AllowedNodes:  set.Of(allowedNodeID),
	})
	require.True(s.IsAllowed(allowedNodeID, true), "Validator should be allowed with validator only rules and allowed nodes")
	require.True(s.IsAllowed(myNodeID, false), "Self node should be allowed with validator only rules")
	require.False(s.IsAllowed(ids.GenerateTestNodeID(), false), "Non-validator should not be allowed with validator only rules and allowed nodes")
	require.True(s.IsAllowed(allowedNodeID, true), "Non-validator allowed node should be allowed with validator only rules and allowed nodes")
}
