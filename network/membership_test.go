// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

func memberCAConfig(t *testing.T, rootPEM []byte) subnets.Config {
	t.Helper()

	return subnets.Config{
		ValidatorOnly: true,
		MemberCA:      []string{string(rootPEM)},
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

func newTestMembership(
	t *testing.T,
	configs map[ids.ID]subnets.Config,
	vdrs validators.Manager,
) *membership {
	t.Helper()

	m, err := newMembership(configs, trackedSubnetsOf(configs), vdrs)
	require.NoError(t, err)
	return m
}

// isCertMember reports certificate membership alone, apart from the validator
// and allowedNodes sources that IsSubnetMember also consults.
func isCertMember(m *membership, subnetID ids.ID, nodeID ids.NodeID) bool {
	return m.certified(nodeID).contains(subnetID, time.Now())
}

func TestMembershipCertSubnets(t *testing.T) {
	var (
		pki           = newTestPKI(t)
		subnetID      = ids.GenerateTestID()
		otherSubnetID = ids.GenerateTestID()
		otherPKI      = newTestPKI(t)
	)

	m := newTestMembership(t, map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   memberCAConfig(t, pki.rootPEM),
		otherSubnetID:              memberCAConfig(t, otherPKI.rootPEM),
	}, validators.NewManager())

	require.Contains(t, m.findCertifiedSubnets(pki.chain), subnetID)
	require.Contains(t, m.findCertifiedSubnets(otherPKI.chain), otherSubnetID)
	require.Empty(t, m.findCertifiedSubnets(stakingtest.SelfSignedChain(t)))
	require.Empty(t, m.findCertifiedSubnets(nil))
}

// TestMembershipNoMemberCA checks that a node whose subnets declare no member
// CA certifies nobody - the stock path.
func TestMembershipNoMemberCA(t *testing.T) {
	pki := newTestPKI(t)

	m := newTestMembership(t, map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		ids.GenerateTestID():       {ValidatorOnly: true},
	}, validators.NewManager())

	require.Nil(t, m.findCertifiedSubnets(pki.chain))
}

func TestMembershipTracking(t *testing.T) {
	require := require.New(t)

	var (
		pki      = newTestPKI(t)
		subnetID = ids.GenerateTestID()
		stranger = ids.GenerateTestNodeID()
	)

	m := newTestMembership(t, map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   memberCAConfig(t, pki.rootPEM),
	}, validators.NewManager())

	require.False(isCertMember(m, subnetID, pki.nodeID))

	m.track(pki.nodeID, m.findCertifiedSubnets(pki.chain))
	require.True(isCertMember(m, subnetID, pki.nodeID))
	require.False(isCertMember(m, ids.GenerateTestID(), pki.nodeID))

	// A peer that is a certificate member of nothing costs no memory.
	m.track(stranger, m.findCertifiedSubnets(stakingtest.SelfSignedChain(t)))
	require.Len(m.certMembers, 1)

	m.untrack(pki.nodeID)
	require.False(isCertMember(m, subnetID, pki.nodeID))
	require.Empty(m.certMembers)
}

// TestMembershipExpiresCertificateMember checks that certificate membership
// stops once the verified chain expires. The peer is then on the wrong message
// stack, which is what closes the connection.
func TestMembershipExpiresCertificateMember(t *testing.T) {
	require := require.New(t)

	var (
		pki      = newTestPKI(t)
		subnetID = ids.GenerateTestID()
	)

	m := newTestMembership(t, map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   memberCAConfig(t, pki.rootPEM),
	}, validators.NewManager())

	m.track(pki.nodeID, m.findCertifiedSubnets(pki.chain))
	require.True(isCertMember(m, subnetID, pki.nodeID))

	// Expiry is read from the record, so an already-expired record stands in
	// for a chain that has aged out since the connection was upgraded.
	m.track(pki.nodeID, certifiedSubnets{subnetID: time.Now().Add(-time.Second)})
	require.False(isCertMember(m, subnetID, pki.nodeID))
	require.False(m.IsSubnetMember(subnetID, pki.nodeID))
}

func TestMembershipIsMember(t *testing.T) {
	var (
		pki       = newTestPKI(t)
		subnetID  = ids.GenerateTestID()
		validator = ids.GenerateTestNodeID()
		allowed   = ids.GenerateTestNodeID()
		stranger  = ids.GenerateTestNodeID()
	)

	subnetConfig := memberCAConfig(t, pki.rootPEM)
	subnetConfig.AllowedNodes = set.Of(allowed)

	vdrs := validators.NewManager()
	require.NoError(t, vdrs.AddStaker(subnetID, validator, nil, ids.GenerateTestID(), 1))

	m := newTestMembership(t, map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   subnetConfig,
	}, vdrs)
	m.track(pki.nodeID, m.findCertifiedSubnets(pki.chain))

	for name, test := range map[string]struct {
		nodeID ids.NodeID
		want   bool
	}{
		"subnet validator":   {nodeID: validator, want: true},
		"certificate member": {nodeID: pki.nodeID, want: true},
		"allowedNodes entry": {nodeID: allowed, want: true},
		"stranger":           {nodeID: stranger, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, m.IsSubnetMember(subnetID, test.nodeID))
			require.Equal(t, test.want, m.isMemberOfAny(test.nodeID))
		})
	}

	// Membership is about the subnets this node tracks, so a primary network
	// validator is a member of nothing here. P-Chain connectivity under
	// network-require-validator-to-connect comes from the ip tracker instead,
	// which wants every primary network validator; see
	// TestAllowConnectionKeepsPrimaryNetworkValidators.
	primaryValidator := ids.GenerateTestNodeID()
	require.NoError(t, vdrs.AddStaker(constants.PrimaryNetworkID, primaryValidator, nil, ids.GenerateTestID(), 1))
	require.False(t, m.IsSubnetMember(subnetID, primaryValidator))
	require.False(t, m.isMemberOfAny(primaryValidator))
}

// TestAllowConnectionKeepsMembers checks that with
// network-require-validator-to-connect on, a non-validator member is kept while
// a stranger is refused. The connection path admits on the freshly verified
// chain alone, before anything is recorded; the ping path reads the record.
func TestAllowConnectionKeepsMembers(t *testing.T) {
	var (
		pki      = newTestPKI(t)
		subnetID = ids.GenerateTestID()
		stranger = ids.GenerateTestNodeID()
	)

	n := newMembershipTestNetwork(t, func(cfg *Config) {
		cfg.RequireValidatorToConnect = true
		cfg.SubnetConfigs = map[ids.ID]subnets.Config{
			constants.PrimaryNetworkID: {},
			subnetID:                   memberCAConfig(t, pki.rootPEM),
		}
	})

	// Nothing is recorded yet, so the ping path sees a stranger while the
	// verified chain is what admits the connection.
	certified := n.membership.findCertifiedSubnets(pki.chain)
	require.NotEmpty(t, certified)
	require.False(t, n.AllowConnection(pki.nodeID))

	// Once recorded, the ping path agrees.
	n.membership.track(pki.nodeID, certified)
	require.True(t, n.AllowConnection(pki.nodeID))
	require.True(t, isCertMember(n.membership, subnetID, pki.nodeID))

	// A self-signed chain proves nothing on either path.
	require.Empty(t, n.membership.findCertifiedSubnets(stakingtest.SelfSignedChain(t)))
	require.False(t, n.AllowConnection(stranger))
}

// TestAllowConnectionKeepsPrimaryNetworkValidators checks that scoping
// membership to tracked subnets does not cut off the P-Chain: a primary network
// validator is a member of nothing, but the ip tracker wants it, so
// network-require-validator-to-connect still keeps the connection.
func TestAllowConnectionKeepsPrimaryNetworkValidators(t *testing.T) {
	require := require.New(t)

	primaryValidator := ids.GenerateTestNodeID()

	n := newMembershipTestNetwork(t, func(cfg *Config) {
		cfg.RequireValidatorToConnect = true
		cfg.SubnetConfigs = map[ids.ID]subnets.Config{
			constants.PrimaryNetworkID: {},
			ids.GenerateTestID():       {ValidatorOnly: true},
		}
	})
	n.config.Validators.RegisterCallbackListener(n.ipTracker)
	require.NoError(n.config.Validators.AddStaker(
		constants.PrimaryNetworkID,
		primaryValidator,
		nil,
		ids.GenerateTestID(),
		1,
	))

	require.False(n.membership.isMemberOfAny(primaryValidator))
	require.True(n.AllowConnection(primaryValidator))
}

func TestAllowConnectionWithoutRequireValidator(t *testing.T) {
	n := newMembershipTestNetwork(t, nil)

	require.False(t, n.config.RequireValidatorToConnect)
	require.True(t, n.AllowConnection(ids.GenerateTestNodeID()))
}

// memberNodeConfig returns a network config for a node whose staking
// certificate was issued by [ca], so that peers trusting the CA's root treat it
// as a member.
func memberNodeConfig(t *testing.T, ca *stakingtest.CA, base Config) *Config {
	t.Helper()
	require := require.New(t)

	certPEM, keyPEM, err := ca.IssueNodeCert("rpc-01", time.Hour)
	require.NoError(err)

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
// the non-validator is admitted under network-require-validator-to-connect and
// lands on the elevated stack, while the connection it replaces would not have.
func TestConnectionGrantsMembership(t *testing.T) {
	require := require.New(t)

	root, nodesCA := stakingtest.NewPKI(t)

	var (
		subnetID     = ids.GenerateTestID()
		elevatedSize = uint32(4 * constants.DefaultMaxMessageSize)

		subnetConfig = subnets.Config{
			ValidatorOnly: true,
			MemberCA:      []string{string(root.CertPEM())},
			LargeMessages: &subnets.LargeMessagesConfig{MaxMessageSize: elevatedSize},
		}
	)

	base := defaultConfig
	base.RequireValidatorToConnect = true
	base.TrackedSubnets = set.Of(subnetID)
	base.SubnetConfigs = map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   subnetConfig,
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
	memberID := memberConfig.MyNodeID

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
			require.FailNow("validator and member never connected")
		}
	}

	// The validator verified the member's chain during the handshake.
	require.True(isCertMember(validatorNet.membership, subnetID, memberID))
	require.Equal(elevatedSize, validatorNet.FrameSize(memberID))

	info := validatorNet.PeerInfo([]ids.NodeID{memberID})
	require.Len(info, 1)
	require.Equal(elevatedSize, info[0].MaxFrameSize)

	// The member elevates the validator too, by its P-Chain entry rather than by
	// a certificate: validators need none.
	require.False(isCertMember(memberNet.membership, subnetID, validatorID))
	require.Equal(elevatedSize, memberNet.FrameSize(validatorID))

	info = memberNet.PeerInfo([]ids.NodeID{validatorID})
	require.Len(info, 1)
	require.Equal(elevatedSize, info[0].MaxFrameSize)

	// A stranger is neither a member nor elevated, and both sides refuse it.
	stranger := ids.GenerateTestNodeID()
	require.False(memberNet.AllowConnection(stranger))
	require.False(validatorNet.AllowConnection(stranger))
	require.Equal(uint32(constants.DefaultMaxMessageSize), memberNet.FrameSize(stranger))
	require.Equal(uint32(constants.DefaultMaxMessageSize), validatorNet.FrameSize(stranger))
}

// TestLogOwnCertificate checks the startup report on this node's own staking
// certificate: silent without a member CA or for the stock self-signed
// certificate, a confirmation naming the subnet for a bundle the roots verify,
// a warning when that membership expires soon, and a warning for anything
// else.
func TestLogOwnCertificate(t *testing.T) {
	// Membership expires with the earliest certificate in the chain, so the
	// roots must outlive the warning window for only the short leaf to trip it.
	const longValidity = 2 * memberCertExpiryWarning
	root, err := stakingtest.NewRootCA("Test Root CA", longValidity)
	require.NoError(t, err)
	nodesCA, err := root.NewIntermediateCA("Test Nodes CA", longValidity)
	require.NoError(t, err)
	_, otherNodesCA := stakingtest.NewPKI(t)
	subnetID := ids.GenerateTestID()

	withMemberCA := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   memberCAConfig(t, root.CertPEM()),
	}
	withoutMemberCA := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID:                   {ValidatorOnly: true},
	}

	issue := func(ca *stakingtest.CA, validity time.Duration) (certPEM, keyPEM []byte) {
		certPEM, keyPEM, err := ca.IssueNodeCert("rpc-01", validity)
		require.NoError(t, err)
		return certPEM, keyPEM
	}
	tlsConfigOf := func(certPEM, keyPEM []byte) *tls.Config {
		tlsCert, err := staking.LoadTLSCertFromBytes(keyPEM, certPEM)
		require.NoError(t, err)
		return peer.TLSConfig(*tlsCert, nil)
	}
	// leafOnly drops the intermediate from a bundle: the likeliest install
	// mistake, a certificate file holding the leaf alone.
	leafOnly := func(certPEM []byte) []byte {
		block, _ := pem.Decode(certPEM)
		require.NotNil(t, block)
		return pem.EncodeToMemory(block)
	}

	selfSigned, err := staking.NewTLSCert()
	require.NoError(t, err)

	bundlePEM, bundleKeyPEM := issue(nodesCA, longValidity)
	shortBundlePEM, shortBundleKeyPEM := issue(nodesCA, time.Hour)
	otherBundlePEM, otherBundleKeyPEM := issue(otherNodesCA, longValidity)

	// The leaf parses, so the file loads, but the intermediate behind it is
	// garbage that every peer's handshake would choke on.
	unparsable := tlsConfigOf(bundlePEM, bundleKeyPEM)
	unparsable.Certificates[0].Certificate = [][]byte{
		unparsable.Certificates[0].Certificate[0],
		[]byte("not a certificate"),
	}

	const (
		verified  = "staking certificate chains to member CA"
		expiring  = "staking certificate membership expires soon"
		rejected  = "staking certificate does not chain to any tracked subnet's member CA"
		malformed = "staking certificate chain does not parse"
	)
	tests := []struct {
		name      string
		configs   map[ids.ID]subnets.Config
		tlsConfig *tls.Config
		wantMsg   string
	}{
		{
			name:      "self-signed is silent",
			configs:   withMemberCA,
			tlsConfig: peer.TLSConfig(*selfSigned, nil),
		},
		{
			name:    "no tls config is silent",
			configs: withMemberCA,
		},
		{
			name:      "bundle verifies",
			configs:   withMemberCA,
			tlsConfig: tlsConfigOf(bundlePEM, bundleKeyPEM),
			wantMsg:   verified,
		},
		{
			name:      "bundle expiring soon",
			configs:   withMemberCA,
			tlsConfig: tlsConfigOf(shortBundlePEM, shortBundleKeyPEM),
			wantMsg:   expiring,
		},
		{
			name:      "leaf without intermediate",
			configs:   withMemberCA,
			tlsConfig: tlsConfigOf(leafOnly(bundlePEM), bundleKeyPEM),
			wantMsg:   rejected,
		},
		{
			name:      "bundle from another root",
			configs:   withMemberCA,
			tlsConfig: tlsConfigOf(otherBundlePEM, otherBundleKeyPEM),
			wantMsg:   rejected,
		},
		{
			name:      "no member CA configured is silent",
			configs:   withoutMemberCA,
			tlsConfig: tlsConfigOf(bundlePEM, bundleKeyPEM),
		},
		{
			name:      "unparsable chain",
			configs:   withMemberCA,
			tlsConfig: unparsable,
			wantMsg:   malformed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			core, logs := observer.New(zapcore.Level(logging.Info))
			m := newTestMembership(t, tt.configs, validators.NewManager())
			m.logOwnCertificate(logging.NewLogger("", logging.WrappedCore{Core: core}), tt.tlsConfig)

			entries := logs.All()
			if tt.wantMsg == "" {
				require.Empty(entries)
				return
			}
			require.Len(entries, 1)
			require.Equal(tt.wantMsg, entries[0].Message)
			switch tt.wantMsg {
			case verified:
				require.Equal(zapcore.Level(logging.Info), entries[0].Level)
				require.Equal(subnetID.String(), entries[0].ContextMap()["subnetID"])
			case expiring:
				require.Equal(zapcore.Level(logging.Warn), entries[0].Level)
				require.Equal(subnetID.String(), entries[0].ContextMap()["subnetID"])
				require.Contains(entries[0].ContextMap(), "expiresAt")
			default:
				require.Equal(zapcore.Level(logging.Warn), entries[0].Level)
			}
		})
	}
}
