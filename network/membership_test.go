// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/staking/stakingtest"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

// testPKI is a root and the chain a node it issued presents during the TLS
// handshake.
type testPKI struct {
	rootPEM []byte
	nodeID  ids.NodeID
	chain   []*x509.Certificate
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()

	root, nodesCA := stakingtest.NewPKI(t)
	chain := stakingtest.NodeChain(t, nodesCA, "rpc-01", stakingtest.DefaultValidity)

	leaf, err := staking.ParseCertificate(chain[0].Raw)
	require.NoError(t, err)

	return testPKI{
		rootPEM: root.CertPEM(),
		nodeID:  ids.NodeIDFromCert(leaf),
		chain:   chain,
	}
}

func membershipCAConfig(t *testing.T, rootPEM []byte) subnets.Config {
	t.Helper()

	return subnets.Config{
		ValidatorOnly: true,
		MembershipCA:  []string{string(rootPEM)},
	}
}

// trackedSubnetsOf returns the subnets a node with [configs] tracks: every
// subnet it holds a config for, except the primary network.
func trackedSubnetsOf(configs map[ids.ID]subnets.Config) set.Set[ids.ID] {
	tracked := set.NewSet[ids.ID](len(configs))
	for subnetID := range configs {
		if subnetID != constants.PrimaryNetworkID {
			tracked.Add(subnetID)
		}
	}
	return tracked
}

func mustNewMembership(t *testing.T, configs map[ids.ID]subnets.Config) *membership {
	t.Helper()

	cert, err := staking.NewTLSCert()
	require.NoError(t, err)
	m, err := newMembership(logging.NoLog{}, peer.TLSConfig(*cert, nil), configs)
	require.NoError(t, err)
	return m
}

// newMembershipTestNetwork returns a network running
// network-require-validator-to-connect with just the pieces that decide
// whether a peer is kept: the config, the membership and the ip tracker.
func newMembershipTestNetwork(t *testing.T, subnetConfigs map[ids.ID]subnets.Config) *network {
	t.Helper()

	cfg, err := NewTestNetworkConfig(
		prometheus.NewRegistry(),
		constants.LocalID,
		validators.NewManager(),
		set.Set[ids.ID]{},
	)
	require.NoError(t, err)
	cfg.RequireValidatorToConnect = true
	cfg.SubnetConfigs = subnetConfigs
	// Node config produces exactly this relationship: a config per tracked
	// subnet, plus the primary network.
	cfg.TrackedSubnets = trackedSubnetsOf(cfg.SubnetConfigs)

	ipTracker, err := newIPTracker(
		cfg.TrackedSubnets,
		logging.NoLog{},
		prometheus.NewRegistry(),
		cfg.ConnectToAllValidators,
	)
	require.NoError(t, err)

	return &network{
		config:     cfg,
		membership: mustNewMembership(t, cfg.SubnetConfigs),
		ipTracker:  ipTracker,
	}
}

// TestMembershipTracking checks the per-connection record: a tracked peer is
// certified for the subnets whose root issued its chain and no other, a peer
// certified by nothing is not stored, membership stops once the verified chain
// expires, and untrack forgets the peer.
func TestMembershipTracking(t *testing.T) {
	require := require.New(t)

	var (
		pki           = newTestPKI(t)
		otherPKI      = newTestPKI(t)
		subnetID      = ids.GenerateTestID()
		otherSubnetID = ids.GenerateTestID()
		stranger      = ids.GenerateTestNodeID()
	)

	m := mustNewMembership(t, map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   membershipCAConfig(t, pki.rootPEM),
		otherSubnetID:              membershipCAConfig(t, otherPKI.rootPEM),
	})

	require.False(m.IsCertified(subnetID, pki.nodeID))

	m.track(pki.nodeID, m.findCertifiedSubnets(pki.chain))
	require.True(m.IsCertified(subnetID, pki.nodeID))
	require.False(m.IsCertified(otherSubnetID, pki.nodeID))

	// A peer that is a certificate member of nothing costs no memory.
	strangerCertified := m.findCertifiedSubnets(stakingtest.SelfSignedChain(t))
	require.Nil(strangerCertified)
	m.track(stranger, strangerCertified)
	require.Len(m.certMembers, 1)

	// Expiry is read from the record, so an already-expired record stands in
	// for a chain that has aged out since the connection was upgraded. Under
	// network-require-validator-to-connect the next Ping then closes the
	// connection.
	m.track(pki.nodeID, certifiedSubnets{subnetID: time.Now().Add(-time.Second)})
	require.False(m.IsCertified(subnetID, pki.nodeID))
	require.False(m.isMemberOfAny(pki.nodeID))

	m.untrack(pki.nodeID)
	require.False(m.IsCertified(subnetID, pki.nodeID))
	require.Empty(m.certMembers)
}

// TestAllowConnectionKeepsMembers checks whom
// network-require-validator-to-connect keeps on a node that does not validate
// the primary network, and which of them a membership CA certified. The ip
// tracker keeps validators without membership knowing them, and allowedNodes
// are kept but not certified, since the subnet admits them from its own config.
func TestAllowConnectionKeepsMembers(t *testing.T) {
	var (
		pki             = newTestPKI(t)
		subnetID        = ids.GenerateTestID()
		allowed         = ids.GenerateTestNodeID()
		subnetValidator = ids.GenerateTestNodeID()
	)

	subnetConfig := membershipCAConfig(t, pki.rootPEM)
	subnetConfig.AllowedNodes = set.Of(allowed)
	n := newMembershipTestNetwork(t, map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   subnetConfig,
	})
	n.config.Validators.RegisterCallbackListener(n.ipTracker)
	require.NoError(t, n.config.Validators.AddStaker(subnetID, subnetValidator, nil, ids.GenerateTestID(), 1))

	tests := map[string]struct {
		nodeID        ids.NodeID
		record        bool
		wantCertified bool
		wantAllowed   bool
	}{
		// The record is written after the de-duplication checks, so the
		// connection path admits on the verified chain, not on this.
		"certificate member before it is recorded": {nodeID: pki.nodeID},
		"certificate member":                       {nodeID: pki.nodeID, record: true, wantCertified: true, wantAllowed: true},
		"allowedNodes entry":                       {nodeID: allowed, wantAllowed: true},
		"subnet validator":                         {nodeID: subnetValidator, wantAllowed: true},
		"stranger":                                 {nodeID: ids.GenerateTestNodeID()},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			if test.record {
				n.membership.track(test.nodeID, n.membership.findCertifiedSubnets(pki.chain))
				defer n.membership.untrack(test.nodeID)
			}
			require.Equal(test.wantCertified, n.membership.IsCertified(subnetID, test.nodeID))
			require.Equal(test.wantAllowed, n.AllowConnection(test.nodeID))
		})
	}
}

// memberNodeConfig returns a network config for a node whose staking
// certificate was issued by [ca], so that peers trusting the CA's root treat it
// as a member.
func memberNodeConfig(t *testing.T, ca *stakingtest.CA, base Config) *Config {
	t.Helper()
	require := require.New(t)

	certPEM, keyPEM := ca.IssueNodeCert(t, "rpc-01", time.Hour)

	tlsCert, err := staking.LoadTLSCertFromBytes(keyPEM, certPEM)
	require.NoError(err)
	// The bundle the node loads carries the issuing CA alongside the leaf; that
	// is what a peer needs to build a chain to the root.
	require.Len(tlsCert.Certificate, 2)

	leaf, err := staking.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(err)

	blsKey, err := localsigner.New()
	require.NoError(err)

	config := base
	config.TLSConfig = peer.TLSConfig(*tlsCert, nil)
	config.MyNodeID = ids.NodeIDFromCert(leaf)
	config.TLSKey = tlsCert.PrivateKey.(crypto.Signer)
	config.BLSKey = blsKey
	return &config
}

// TestConnectionGrantsMembership drives two real networks through a TLS
// handshake and checks that the certificate chain the member sends survives it:
// the non-validator is admitted under network-require-validator-to-connect on
// its certificate alone.
func TestConnectionGrantsMembership(t *testing.T) {
	require := require.New(t)

	root, nodesCA := stakingtest.NewPKI(t)
	subnetID := ids.GenerateTestID()

	base := defaultConfig
	base.RequireValidatorToConnect = true
	base.TrackedSubnets = set.Of(subnetID)
	base.SubnetConfigs = map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   membershipCAConfig(t, root.CertPEM()),
	}

	// The validator keeps its self-signed certificate; only the non-validator
	// needs one from the CA. The validator deliberately does not validate the
	// primary network, which would make it keep every connection: its admission
	// of the member must rest on the certificate alone.
	dialer, listeners, validatorIDs, validatorConfigs := newTestNetwork(t, 1, base)
	validatorConfig := validatorConfigs[0]
	validatorID := validatorIDs[0]

	memberIP, memberListener := dialer.NewListener()
	memberConfig := memberNodeConfig(t, nodesCA, base)
	memberConfig.MyIPPort = utils.NewAtomic(memberIP)

	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(subnetID, validatorID, nil, ids.GenerateTestID(), 1))

	connected := make(chan ids.NodeID, 2)
	networks := make([]*network, 0, 2)
	for _, node := range []struct {
		config   *Config
		listener net.Listener
	}{
		{config: validatorConfig, listener: listeners[0]},
		{config: memberConfig, listener: memberListener},
	} {
		node.config.Validators = vdrs
		node.config.Beacons = validators.NewManager()

		net, err := NewNetwork(
			node.config,
			upgrade.InitiallyActiveTime,
			newMessageCreator(t),
			prometheus.NewRegistry(),
			logging.NoLog{},
			node.listener,
			dialer,
			&testHandler{
				InboundHandler: nil,
				ConnectedF: func(nodeID ids.NodeID, _ *version.Application, subnet ids.ID) {
					if subnet == constants.PrimaryNetworkID {
						connected <- nodeID
					}
				},
				DisconnectedF: func(ids.NodeID) {},
			},
		)
		require.NoError(err)
		networks = append(networks, net.(*network))
	}

	validatorNet, memberNet := networks[0], networks[1]

	eg := &errgroup.Group{}
	memberNet.ManuallyTrack(validatorID, validatorConfig.MyIPPort.Get())
	for _, net := range networks {
		eg.Go(net.Dispatch)
	}
	defer func() {
		for _, net := range networks {
			net.StartClose()
		}
		require.NoError(eg.Wait())
	}()

	for range 2 {
		select {
		case <-connected:
		case <-time.After(10 * time.Second):
			t.Fatal("validator and member never connected")
		}
	}

	require.True(validatorNet.membership.IsCertified(subnetID, memberConfig.MyNodeID))
}
