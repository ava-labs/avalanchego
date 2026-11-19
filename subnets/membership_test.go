// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// testMembers is a MembershipChecker backed by a fixed set per subnet, standing
// in for the network layer.
type testMembers map[ids.ID]set.Set[ids.NodeID]

func (m testMembers) IsCertified(subnetID ids.ID, nodeID ids.NodeID) bool {
	members := m[subnetID]
	return members.Contains(nodeID)
}

// TestIsAllowedMembers covers the clause NewWithMembership adds to IsAllowed: a
// non-validator the network reports as a member is admitted to a
// validator-only subnet, and only to that subnet.
func TestIsAllowedMembers(t *testing.T) {
	var (
		myNodeID    = ids.GenerateTestNodeID()
		subnetID    = ids.GenerateTestID()
		member      = ids.GenerateTestNodeID()
		stranger    = ids.GenerateTestNodeID()
		otherMember = ids.GenerateTestNodeID()

		members = testMembers{
			subnetID:             set.Of(member),
			ids.GenerateTestID(): set.Of(otherMember),
		}
	)

	tests := map[string]struct {
		subnet  Subnet
		nodeID  ids.NodeID
		allowed bool
	}{
		"member": {
			subnet:  NewWithMembership(myNodeID, subnetID, Config{ValidatorOnly: true}, members),
			nodeID:  member,
			allowed: true,
		},
		"stranger": {
			subnet: NewWithMembership(myNodeID, subnetID, Config{ValidatorOnly: true}, members),
			nodeID: stranger,
		},
		"member of another subnet": {
			subnet: NewWithMembership(myNodeID, subnetID, Config{ValidatorOnly: true}, members),
			nodeID: otherMember,
		},
		"member of an open subnet": {
			subnet:  NewWithMembership(myNodeID, subnetID, Config{}, members),
			nodeID:  stranger,
			allowed: true,
		},
		"New knows no members": {
			subnet: New(myNodeID, Config{ValidatorOnly: true}),
			nodeID: member,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.allowed, test.subnet.IsAllowed(test.nodeID, false))
		})
	}
}
