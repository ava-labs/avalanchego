// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"crypto"
	"crypto/x509"
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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/constants"
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
	cert           *x509.Certificate
	nodeID         ids.NodeID
	inboundMsgChan <-chan message.InboundMessage
}

func newMessageCreator(t *testing.T) message.Creator {
	t.Helper()

	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		"",
		true,
		10*time.Second,
	)
	require.NoError(t, err)

	return mc
}

func makeRawTestPeers(t *testing.T) (*rawTestPeer, *rawTestPeer) {
	t.Helper()
	require := require.New(t)

	conn0, conn1 := net.Pipe()

	tlsCert0, err := staking.NewTLSCert()
	require.NoError(err)

	tlsCert1, err := staking.NewTLSCert()
	require.NoError(err)

	nodeID0 := ids.NodeIDFromCert(tlsCert0.Leaf)
	nodeID1 := ids.NodeIDFromCert(tlsCert1.Leaf)

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
		MySubnets:            set.Set[ids.ID]{},
		Beacons:              validators.NewSet(),
		NetworkID:            constants.LocalID,
		PingFrequency:        constants.DefaultPingFrequency,
		PongTimeout:          constants.DefaultPingPongTimeout,
		MaxClockDifference:   time.Minute,
		ResourceTracker:      resourceTracker,
	}
	peerConfig0 := sharedConfig
	peerConfig1 := sharedConfig

	ip0 := ips.NewDynamicIPPort(net.IPv6loopback, 0)
	tls0 := tlsCert0.PrivateKey.(crypto.Signer)
	peerConfig0.IPSigner = NewIPSigner(ip0, tls0)

	peerConfig0.Network = TestNetwork
	inboundMsgChan0 := make(chan message.InboundMessage)
	peerConfig0.Router = router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
		inboundMsgChan0 <- msg
	})

	ip1 := ips.NewDynamicIPPort(net.IPv6loopback, 1)
	tls1 := tlsCert1.PrivateKey.(crypto.Signer)
	peerConfig1.IPSigner = NewIPSigner(ip1, tls1)

	peerConfig1.Network = TestNetwork
	inboundMsgChan1 := make(chan message.InboundMessage)
	peerConfig1.Router = router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
		inboundMsgChan1 <- msg
	})

	peer0 := &rawTestPeer{
		config:         &peerConfig0,
		conn:           conn0,
		cert:           tlsCert0.Leaf,
		nodeID:         nodeID0,
		inboundMsgChan: inboundMsgChan0,
	}
	peer1 := &rawTestPeer{
		config:         &peerConfig1,
		conn:           conn1,
		cert:           tlsCert1.Leaf,
		nodeID:         nodeID1,
		inboundMsgChan: inboundMsgChan1,
	}
	return peer0, peer1
}

func makeTestPeers(t *testing.T) (*testPeer, *testPeer) {
	rawPeer0, rawPeer1 := makeRawTestPeers(t)

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

func makeReadyTestPeers(t *testing.T) (*testPeer, *testPeer) {
	t.Helper()
	require := require.New(t)

	peer0, peer1 := makeTestPeers(t)

	err := peer0.AwaitReady(context.Background())
	require.NoError(err)
	isReady := peer0.Ready()
	require.True(isReady)

	err = peer1.AwaitReady(context.Background())
	require.NoError(err)
	isReady = peer1.Ready()
	require.True(isReady)

	return peer0, peer1
}

func TestReady(t *testing.T) {
	require := require.New(t)

	rawPeer0, rawPeer1 := makeRawTestPeers(t)

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

	isReady := peer0.Ready()
	require.False(isReady)

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

	err := peer0.AwaitReady(context.Background())
	require.NoError(err)
	isReady = peer0.Ready()
	require.True(isReady)

	err = peer1.AwaitReady(context.Background())
	require.NoError(err)
	isReady = peer1.Ready()
	require.True(isReady)

	peer0.StartClose()
	err = peer0.AwaitClosed(context.Background())
	require.NoError(err)
	err = peer1.AwaitClosed(context.Background())
	require.NoError(err)
}

func TestSend(t *testing.T) {
	require := require.New(t)

	peer0, peer1 := makeReadyTestPeers(t)
	mc := newMessageCreator(t)

	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	require.NoError(err)

	sent := peer0.Send(context.Background(), outboundGetMsg)
	require.True(sent)

	inboundGetMsg := <-peer1.inboundMsgChan
	require.Equal(message.GetOp, inboundGetMsg.Op())

	peer1.StartClose()
	err = peer0.AwaitClosed(context.Background())
	require.NoError(err)
	err = peer1.AwaitClosed(context.Background())
	require.NoError(err)
}
