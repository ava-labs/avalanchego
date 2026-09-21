// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	s := New(myNodeID, ids.Empty, Config{}, NoOpMembershipChecker)
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

// testMembers is a MembershipChecker backed by a fixed set per subnet, standing
// in for the network layer, which resolves validator status, certificate
// membership, and allowedNodes into one answer.
type testMembers map[ids.ID]set.Set[ids.NodeID]

func (m testMembers) IsSubnetMember(subnetID ids.ID, nodeID ids.NodeID) bool {
	members := m[subnetID]
	return members.Contains(nodeID)
}

func TestIsAllowed(t *testing.T) {
	var (
		myNodeID = ids.GenerateTestNodeID()
		subnetID = ids.GenerateTestID()
		member   = ids.GenerateTestNodeID()
		stranger = ids.GenerateTestNodeID()

		members = testMembers{subnetID: set.Of(member)}
	)

	tests := map[string]struct {
		config  Config
		nodeID  ids.NodeID
		allowed bool
	}{
		"open subnet, member": {
			nodeID:  member,
			allowed: true,
		},
		"open subnet, stranger": {
			nodeID:  stranger,
			allowed: true,
		},
		"validator only, member": {
			config:  Config{ValidatorOnly: true},
			nodeID:  member,
			allowed: true,
		},
		"validator only, self": {
			config:  Config{ValidatorOnly: true},
			nodeID:  myNodeID,
			allowed: true,
		},
		"validator only, stranger": {
			config: Config{ValidatorOnly: true},
			nodeID: stranger,
		},
		"validator only, member of another subnet": {
			config: Config{ValidatorOnly: true},
			nodeID: ids.GenerateTestNodeID(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			s := New(myNodeID, subnetID, test.config, members)
			require.Equal(test.allowed, s.IsAllowed(test.nodeID))
		})
	}
}
