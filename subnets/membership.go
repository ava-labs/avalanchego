// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import "github.com/ava-labs/avalanchego/ids"

// MembershipChecker reports whether a peer's staking certificate chain made it a
// member of a subnet when it connected. It is implemented by the network layer,
// which verifies the chain during the TLS handshake. A subnet's validators and
// allowedNodes are not its concern: [Subnet.IsAllowed] admits those itself.
type MembershipChecker interface {
	IsCertified(subnetID ids.ID, nodeID ids.NodeID) bool
}

// NoOpMembershipChecker certifies no peer, so a Subnet given it admits
// validators and allowedNodes only, as one built by [New] does.
var NoOpMembershipChecker MembershipChecker = noOpMembershipChecker{}

type noOpMembershipChecker struct{}

func (noOpMembershipChecker) IsCertified(ids.ID, ids.NodeID) bool {
	return false
}

// NewWithMembership returns the Subnet [subnetID] as this node runs it, admitting
// the peers [members] certifies on top of its validators and allowedNodes.
func NewWithMembership(
	myNodeID ids.NodeID,
	subnetID ids.ID,
	config Config,
	members MembershipChecker,
) Subnet {
	s := New(myNodeID, config).(*subnet)
	s.subnetID = subnetID
	s.members = members
	return s
}
