// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"crypto"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

type testPeer struct {
	Peer
	inboundMsgChan <-chan message.InboundMessage
}

type rawTestPeer struct {
	config         *Config
	conn           net.Conn
	cert           *staking.Certificate
	nodeID         ids.NodeID
	inboundMsgChan <-chan message.InboundMessage
}

func newMessageCreator(t *testing.T) message.Creator {
	t.Helper()

	mc, err := message.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		"",
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	return mc
}

func makeRawTestPeers(t *testing.T, trackedSubnets set.Set[ids.ID]) (*rawTestPeer, *rawTestPeer) {
	t.Helper()
	require := require.New(t)

	conn0, conn1 := net.Pipe()

	tlsCert0, err := staking.NewTLSCert()
	require.NoError(err)
	cert0, err := staking.ParseCertificate(tlsCert0.Leaf.Raw)
	require.NoError(err)

	tlsCert1, err := staking.NewTLSCert()
	require.NoError(err)
	cert1, err := staking.ParseCertificate(tlsCert1.Leaf.Raw)
	require.NoError(err)

	nodeID0 := ids.NodeIDFromCert(cert0)
	nodeID1 := ids.NodeIDFromCert(cert1)

	mc := newMessageCreator(t)

	metrics, err := NewMetrics(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		10*time.Second,
	)
	require.NoError(err)

	sharedConfig := Config{
		Metrics:              metrics,
		MessageCreator:       mc,
		Log:                  logging.NoLog{},
		InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
		VersionCompatibility: version.GetCompatibility(constants.LocalID),
		MySubnets:            trackedSubnets,
		UptimeCalculator:     uptime.NoOpCalculator,
		Beacons:              validators.NewManager(),
		Validators:           validators.NewManager(),
		NetworkID:            constants.LocalID,
		PingFrequency:        constants.DefaultPingFrequency,
		PongTimeout:          constants.DefaultPingPongTimeout,
		MaxClockDifference:   time.Minute,
		ResourceTracker:      resourceTracker,
	}
	peerConfig0 := sharedConfig
	peerConfig1 := sharedConfig

	ip0 := ips.NewDynamicIPPort(net.IPv6loopback, 1)
	tls0 := tlsCert0.PrivateKey.(crypto.Signer)
	bls0, err := bls.NewSecretKey()
	require.NoError(err)

	peerConfig0.IPSigner = NewIPSigner(ip0, tls0, bls0)

	peerConfig0.Network = TestNetwork
	inboundMsgChan0 := make(chan message.InboundMessage)
	peerConfig0.Router = router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
		inboundMsgChan0 <- msg
	})

	ip1 := ips.NewDynamicIPPort(net.IPv6loopback, 2)
	tls1 := tlsCert1.PrivateKey.(crypto.Signer)
	bls1, err := bls.NewSecretKey()
	require.NoError(err)

	peerConfig1.IPSigner = NewIPSigner(ip1, tls1, bls1)

	peerConfig1.Network = TestNetwork
	inboundMsgChan1 := make(chan message.InboundMessage)
	peerConfig1.Router = router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
		inboundMsgChan1 <- msg
	})

	peer0 := &rawTestPeer{
		config:         &peerConfig0,
		conn:           conn0,
		cert:           cert0,
		nodeID:         nodeID0,
		inboundMsgChan: inboundMsgChan0,
	}
	peer1 := &rawTestPeer{
		config:         &peerConfig1,
		conn:           conn1,
		cert:           cert1,
		nodeID:         nodeID1,
		inboundMsgChan: inboundMsgChan1,
	}
	return peer0, peer1
}

func makeTestPeers(t *testing.T, trackedSubnets set.Set[ids.ID]) (*testPeer, *testPeer) {
	rawPeer0, rawPeer1 := makeRawTestPeers(t, trackedSubnets)

	peer0 := &testPeer{
		Peer: Start(
			rawPeer0.config,
			rawPeer0.conn,
			rawPeer1.cert,
			rawPeer1.nodeID,
			NewThrottledMessageQueue(
				rawPeer0.config.Metrics,
				rawPeer1.nodeID,
				logging.NoLog{},
				throttling.NewNoOutboundThrottler(),
			),
		),
		inboundMsgChan: rawPeer0.inboundMsgChan,
	}
	peer1 := &testPeer{
		Peer: Start(
			rawPeer1.config,
			rawPeer1.conn,
			rawPeer0.cert,
			rawPeer0.nodeID,
			NewThrottledMessageQueue(
				rawPeer1.config.Metrics,
				rawPeer0.nodeID,
				logging.NoLog{},
				throttling.NewNoOutboundThrottler(),
			),
		),
		inboundMsgChan: rawPeer1.inboundMsgChan,
	}
	return peer0, peer1
}

func makeReadyTestPeers(t *testing.T, trackedSubnets set.Set[ids.ID]) (*testPeer, *testPeer) {
	t.Helper()
	require := require.New(t)

	peer0, peer1 := makeTestPeers(t, trackedSubnets)

	require.NoError(peer0.AwaitReady(context.Background()))
	require.True(peer0.Ready())

	require.NoError(peer1.AwaitReady(context.Background()))
	require.True(peer1.Ready())

	return peer0, peer1
}

func TestReady(t *testing.T) {
	require := require.New(t)

	rawPeer0, rawPeer1 := makeRawTestPeers(t, set.Set[ids.ID]{})
	peer0 := Start(
		rawPeer0.config,
		rawPeer0.conn,
		rawPeer1.cert,
		rawPeer1.nodeID,
		NewThrottledMessageQueue(
			rawPeer0.config.Metrics,
			rawPeer1.nodeID,
			logging.NoLog{},
			throttling.NewNoOutboundThrottler(),
		),
	)

	require.False(peer0.Ready())

	peer1 := Start(
		rawPeer1.config,
		rawPeer1.conn,
		rawPeer0.cert,
		rawPeer0.nodeID,
		NewThrottledMessageQueue(
			rawPeer1.config.Metrics,
			rawPeer0.nodeID,
			logging.NoLog{},
			throttling.NewNoOutboundThrottler(),
		),
	)

	require.NoError(peer0.AwaitReady(context.Background()))
	require.True(peer0.Ready())

	require.NoError(peer1.AwaitReady(context.Background()))
	require.True(peer1.Ready())

	peer0.StartClose()
	require.NoError(peer0.AwaitClosed(context.Background()))
	require.NoError(peer1.AwaitClosed(context.Background()))
}

func TestSend(t *testing.T) {
	require := require.New(t)

	peer0, peer1 := makeReadyTestPeers(t, set.Set[ids.ID]{})
	mc := newMessageCreator(t)

	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	require.NoError(err)

	require.True(peer0.Send(context.Background(), outboundGetMsg))

	inboundGetMsg := <-peer1.inboundMsgChan
	require.Equal(message.GetOp, inboundGetMsg.Op())

	peer1.StartClose()
	require.NoError(peer0.AwaitClosed(context.Background()))
	require.NoError(peer1.AwaitClosed(context.Background()))
}

func TestPingUptimes(t *testing.T) {
	trackedSubnetID := ids.GenerateTestID()
	untrackedSubnetID := ids.GenerateTestID()

	trackedSubnets := set.Of(trackedSubnetID)

	mc := newMessageCreator(t)

	testCases := []struct {
		name        string
		msg         message.OutboundMessage
		shouldClose bool
		assertFn    func(*require.Assertions, *testPeer)
	}{
		{
			name: "primary network only",
			msg: func() message.OutboundMessage {
				pingMsg, err := mc.Ping(1, nil)
				require.NoError(t, err)
				return pingMsg
			}(),
			assertFn: func(require *require.Assertions, peer *testPeer) {
				uptime, ok := peer.ObservedUptime(constants.PrimaryNetworkID)
				require.True(ok)
				require.Equal(uint32(1), uptime)

				uptime, ok = peer.ObservedUptime(trackedSubnetID)
				require.False(ok)
				require.Zero(uptime)
			},
		},
		{
			name: "primary network and subnet",
			msg: func() message.OutboundMessage {
				pingMsg, err := mc.Ping(
					1,
					[]*p2p.SubnetUptime{
						{
							SubnetId: trackedSubnetID[:],
							Uptime:   1,
						},
					},
				)
				require.NoError(t, err)
				return pingMsg
			}(),
			assertFn: func(require *require.Assertions, peer *testPeer) {
				uptime, ok := peer.ObservedUptime(constants.PrimaryNetworkID)
				require.True(ok)
				require.Equal(uint32(1), uptime)

				uptime, ok = peer.ObservedUptime(trackedSubnetID)
				require.True(ok)
				require.Equal(uint32(1), uptime)
			},
		},
		{
			name: "primary network and non tracked subnet",
			msg: func() message.OutboundMessage {
				pingMsg, err := mc.Ping(
					1,
					[]*p2p.SubnetUptime{
						{
							// Providing the untrackedSubnetID here should cause
							// the remote peer to disconnect from us.
							SubnetId: untrackedSubnetID[:],
							Uptime:   1,
						},
						{
							SubnetId: trackedSubnetID[:],
							Uptime:   1,
						},
					},
				)
				require.NoError(t, err)
				return pingMsg
			}(),
			shouldClose: true,
		},
	}

	// Note: we reuse peers across tests because makeReadyTestPeers takes awhile
	// to run.
	peer0, peer1 := makeReadyTestPeers(t, trackedSubnets)
	defer func() {
		peer1.StartClose()
		peer0.StartClose()
		require.NoError(t, peer0.AwaitClosed(context.Background()))
		require.NoError(t, peer1.AwaitClosed(context.Background()))
	}()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)

			require.True(peer0.Send(context.Background(), tc.msg))

			// Note: shouldClose can only be `true` for the last test because
			// we reuse peers across tests.
			if tc.shouldClose {
				require.NoError(peer1.AwaitClosed(context.Background()))
				return
			}

			// we send Get message after ping to ensure Ping is handled by the
			// time Get is handled. This is because Get is routed to the handler
			// whereas Ping is handled by the peer directly. We have no way to
			// know when the peer has handled the Ping message.
			sendAndFlush(t, peer0, peer1)

			tc.assertFn(require, peer1)
		})
	}
}

// Test that a peer using the wrong BLS key is disconnected from.
func TestInvalidBLSKeyDisconnects(t *testing.T) {
	require := require.New(t)

	rawPeer0, rawPeer1 := makeRawTestPeers(t, nil)
	require.NoError(rawPeer0.config.Validators.AddStaker(
		constants.PrimaryNetworkID,
		rawPeer1.nodeID,
		bls.PublicFromSecretKey(rawPeer1.config.IPSigner.blsSigner),
		ids.GenerateTestID(),
		1,
	))

	bogusBLSKey, err := bls.NewSecretKey()
	require.NoError(err)
	require.NoError(rawPeer1.config.Validators.AddStaker(
		constants.PrimaryNetworkID,
		rawPeer0.nodeID,
		bls.PublicFromSecretKey(bogusBLSKey), // This is the wrong BLS key for this peer
		ids.GenerateTestID(),
		1,
	))
	peer0 := &testPeer{
		Peer: Start(
			rawPeer0.config,
			rawPeer0.conn,
			rawPeer1.cert,
			rawPeer1.nodeID,
			NewThrottledMessageQueue(
				rawPeer0.config.Metrics,
				rawPeer1.nodeID,
				logging.NoLog{},
				throttling.NewNoOutboundThrottler(),
			),
		),
		inboundMsgChan: rawPeer0.inboundMsgChan,
	}
	peer1 := &testPeer{
		Peer: Start(
			rawPeer1.config,
			rawPeer1.conn,
			rawPeer0.cert,
			rawPeer0.nodeID,
			NewThrottledMessageQueue(
				rawPeer1.config.Metrics,
				rawPeer0.nodeID,
				logging.NoLog{},
				throttling.NewNoOutboundThrottler(),
			),
		),
		inboundMsgChan: rawPeer1.inboundMsgChan,
	}

	// Because peer1 thinks that peer0 is using the wrong BLS key, they should
	// disconnect from each other.
	require.NoError(peer0.AwaitClosed(context.Background()))
	require.NoError(peer1.AwaitClosed(context.Background()))
}

func TestShouldDisconnect(t *testing.T) {
	peerID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()
	blsKey, err := bls.NewSecretKey()
	require.NoError(t, err)

	tests := []struct {
		name                     string
		initialPeer              *peer
		expectedPeer             *peer
		expectedShouldDisconnect bool
	}{
		{
			name: "peer is reporting old version",
			initialPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
				},
				version: &version.Application{
					Name:  version.Client,
					Major: 0,
					Minor: 0,
					Patch: 0,
				},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
				},
				version: &version.Application{
					Name:  version.Client,
					Major: 0,
					Minor: 0,
					Patch: 0,
				},
			},
			expectedShouldDisconnect: true,
		},
		{
			name: "peer is not a validator",
			initialPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators:           validators.NewManager(),
				},
				version: version.CurrentApp,
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators:           validators.NewManager(),
				},
				version: version.CurrentApp,
			},
			expectedShouldDisconnect: false,
		},
		{
			name: "peer is a validator without a BLS key",
			initialPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							nil,
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							nil,
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
			},
			expectedShouldDisconnect: false,
		},
		{
			name: "already verified peer",
			initialPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:                   peerID,
				version:              version.CurrentApp,
				txIDOfVerifiedBLSKey: txID,
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:                   peerID,
				version:              version.CurrentApp,
				txIDOfVerifiedBLSKey: txID,
			},
			expectedShouldDisconnect: false,
		},
		{
			name: "peer without signature",
			initialPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip:      &SignedIP{},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip:      &SignedIP{},
			},
			expectedShouldDisconnect: true,
		},
		{
			name: "peer with invalid signature",
			initialPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: bls.SignProofOfPossession(blsKey, []byte("wrong message")),
				},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: bls.SignProofOfPossession(blsKey, []byte("wrong message")),
				},
			},
			expectedShouldDisconnect: true,
		},
		{
			name: "peer with valid signature",
			initialPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: bls.SignProofOfPossession(blsKey, (&UnsignedIP{}).bytes()),
				},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  logging.NoLog{},
					VersionCompatibility: version.GetCompatibility(constants.UnitTestID),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, vdrs.AddStaker(
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicFromSecretKey(blsKey),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: bls.SignProofOfPossession(blsKey, (&UnsignedIP{}).bytes()),
				},
				txIDOfVerifiedBLSKey: txID,
			},
			expectedShouldDisconnect: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			shouldDisconnect := test.initialPeer.shouldDisconnect()
			require.Equal(test.expectedPeer, test.initialPeer)
			require.Equal(test.expectedShouldDisconnect, shouldDisconnect)
		})
	}
}

// Helper to send a message from sender to receiver and assert that the
// receiver receives the message. This can be used to test a prior message
// was handled by the peer.
func sendAndFlush(t *testing.T, sender *testPeer, receiver *testPeer) {
	t.Helper()
	mc := newMessageCreator(t)
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	require.NoError(t, err)
	require.True(t, sender.Send(context.Background(), outboundGetMsg))
	inboundGetMsg := <-receiver.inboundMsgChan
	require.Equal(t, message.GetOp, inboundGetMsg.Op())
}
