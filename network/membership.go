// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/x509"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ subnets.MembershipChecker = (*membership)(nil)

// certifiedSubnets records, for one peer, each subnet whose member CA verified
// its staking certificate chain and when that verification expires.
//
// A record is never mutated once built, so a reference to one may be read
// after the lock that handed it out is released.
type certifiedSubnets map[ids.ID]time.Time

// contains reports whether [subnetID] certified the peer and has not expired
// as of [now]. A nil map certifies nothing.
func (c certifiedSubnets) contains(subnetID ids.ID, now time.Time) bool {
	expiresAt, ok := c[subnetID]
	return ok && now.Before(expiresAt)
}

// membership answers, for each subnet this node tracks, whether a peer is a
// member of it: a validator of the subnet, a peer whose staking certificate
// chains to the subnet's member CA, or a peer listed in the subnet's
// allowedNodes.
//
// Certificate membership is fixed for the life of a connection - a node ID is a
// hash of the leaf certificate, so a peer cannot present a different chain
// without becoming a different node - and is therefore decided once while the
// TLS handshake still has the chain in hand. It is dropped when the peer
// disconnects, and ignored once the verified chain expires. Validator
// membership changes underneath a live connection and is read from the
// validator manager on every call.
type membership struct {
	// memberCAs holds the member CA of every tracked subnet that declares one,
	// and allowedNodes the allowedNodes of every subnet that lists any.
	//
	// Invariant: memberCAs covers tracked subnets only, so that a chain
	// [findCertifiedSubnets] verifies proves membership of a tracked subnet.
	// [network.upgrade] admits a connection on that alone.
	memberCAs    map[ids.ID]*subnets.MemberCA
	allowedNodes map[ids.ID]set.Set[ids.NodeID]
	// trackedSubnets are the subnets this node runs chains for. It never
	// contains the primary network, which is what scopes connection admission
	// to the private subnets membership is about.
	trackedSubnets set.Set[ids.ID]
	validators     validators.Manager

	lock sync.RWMutex
	// certMembers maps a peer to the subnets that certified it when the
	// connection was upgraded. An expired entry is ignored on read and removed
	// when the peer disconnects.
	certMembers map[ids.NodeID]certifiedSubnets
}

func newMembership(
	subnetConfigs map[ids.ID]subnets.Config,
	trackedSubnets set.Set[ids.ID],
	vdrs validators.Manager,
) (*membership, error) {
	m := &membership{
		memberCAs:      make(map[ids.ID]*subnets.MemberCA),
		allowedNodes:   make(map[ids.ID]set.Set[ids.NodeID]),
		trackedSubnets: trackedSubnets,
		validators:     vdrs,
		certMembers:    make(map[ids.NodeID]certifiedSubnets),
	}
	for subnetID, config := range subnetConfigs {
		ca, err := config.LoadMemberCA()
		if err != nil {
			return nil, fmt.Errorf("loading member CA of subnet %s: %w", subnetID, err)
		}
		if ca != nil && trackedSubnets.Contains(subnetID) {
			m.memberCAs[subnetID] = ca
		}
		if config.AllowedNodes.Len() > 0 {
			m.allowedNodes[subnetID] = config.AllowedNodes
		}
	}
	return m, nil
}

// findCertifiedSubnets returns the subnets whose member CA verifies [chain], a
// peer's certificate chain as crypto/tls reports it, leaf first.
//
// This is the only signature-verifying step on the connection path, so it runs
// once per upgraded connection: [membership.track] records the result, and
// every later consumer reads the record.
func (m *membership) findCertifiedSubnets(chain []*x509.Certificate) certifiedSubnets {
	var certified certifiedSubnets
	for subnetID, ca := range m.memberCAs {
		expiresAt, ok := ca.VerifyUntil(chain)
		if !ok {
			continue
		}
		if certified == nil {
			certified = make(certifiedSubnets)
		}
		certified[subnetID] = expiresAt
	}
	return certified
}

// track records what [nodeID]'s chain proved. An empty [verified] records
// nothing, so peers that proved nothing - every stock peer - cost no memory.
func (m *membership) track(nodeID ids.NodeID, verified certifiedSubnets) {
	if len(verified) == 0 {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	m.certMembers[nodeID] = verified
}

func (m *membership) untrack(nodeID ids.NodeID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	delete(m.certMembers, nodeID)
}

// certified returns what [nodeID]'s chain proved when it connected, or nil for
// a peer with no record - every stock peer.
func (m *membership) certified(nodeID ids.NodeID) certifiedSubnets {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.certMembers[nodeID]
}

// IsSubnetMember reports whether [nodeID] is a member of [subnetID], reading
// certificate membership from the record of its connection.
//
// This is deliberately the one predicate behind subnet admission, the elevated
// message stack and connection admission, so that the chains this node runs
// ask the same question the network answers for itself, every admitted peer is
// elevated at the same size, and no unadmitted peer holds an elevated throttler
// allocation.
//
// An expired record is simply not a member: every read checks the expiry, so
// leaving it in place until the peer disconnects saves sweeping the map on a
// timer. Expiry alone does not close the connection; the next Ping does, if
// the peer is then on the wrong stack or no longer wanted under
// network-require-validator-to-connect.
func (m *membership) IsSubnetMember(subnetID ids.ID, nodeID ids.NodeID) bool {
	// Validators, the common case on the per-message path, never reach the
	// record's lock.
	if _, isValidator := m.validators.GetValidator(subnetID, nodeID); isValidator {
		return true
	}
	if m.certified(nodeID).contains(subnetID, time.Now()) {
		return true
	}
	allowedNodes := m.allowedNodes[subnetID]
	return allowedNodes.Contains(nodeID)
}

// isMemberOfAny reports whether [nodeID] is a member of any subnet this node
// tracks. It is what lets network-require-validator-to-connect keep the
// non-validator members of a private subnet.
//
// The primary network is deliberately not consulted: its validators are already
// wanted by the ip tracker, which [network.AllowConnection] checks first.
func (m *membership) isMemberOfAny(nodeID ids.NodeID) bool {
	for subnetID := range m.trackedSubnets {
		if m.IsSubnetMember(subnetID, nodeID) {
			return true
		}
	}
	return false
}
