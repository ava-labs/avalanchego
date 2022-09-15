// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package test

import (
	"context"
	"crypto"
	"crypto/tls"
	"net"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	network_peer "github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/version"
)

const maxMessageToSend = 1024

type Op struct {
	networkID   uint32
	stakingKey  string
	stakingCert string
	useProto    bool
}

type OpFunc func(*Op)

func (op *Op) applyOpts(opts []OpFunc) {
	for _, opt := range opts {
		opt(op)
	}
}

func WithNetworkID(networkID uint32) OpFunc {
	return func(op *Op) {
		op.networkID = networkID
	}
}

func WithStakingCert(key string, cert string) OpFunc {
	return func(op *Op) {
		op.stakingKey = key
		op.stakingCert = cert
	}
}

// "true" to use protobufs message creator.
func WithProtoMessage(useProto bool) OpFunc {
	return func(op *Op) {
		op.useProto = useProto
	}
}

// Start provides a simple interface to create a peer that has finished
// the p2p handshake.
//
// This function will generate a new TLS key to use when connecting to the peer.
//
// The returned peer will not throttle inbound or outbound messages.
//
//   - [ctx] provides a way of canceling the connection request.
//   - [ip] is the remote that will be dialed to create the connection.
//   - [networkID] will be sent to the peer during the handshake. If the peer is
//     expecting a different [networkID], the handshake will fail and an error
//     will be returned.
//   - [router] will be called with all non-handshake messages received by the
//     peer.
func Start(ctx context.Context, ip ips.IPPort, router router.InboundHandler, opts ...OpFunc) (network_peer.Peer, error) {
	ret := &Op{networkID: constants.LocalID}
	ret.applyOpts(opts)

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, constants.NetworkType, ip.String())
	if err != nil {
		return nil, err
	}

	var tlsCert *tls.Certificate
	if ret.stakingKey != "" && ret.stakingCert != "" {
		tlsCert, err = staking.LoadTLSCertFromFiles(ret.stakingKey, ret.stakingCert)
	} else {
		tlsCert, err = staking.NewTLSCert()
	}
	if err != nil {
		return nil, err
	}

	tlsConfg := network_peer.TLSConfig(*tlsCert, nil)
	clientUpgrader := network_peer.NewTLSClientUpgrader(tlsConfg)

	peerID, conn, cert, err := clientUpgrader.Upgrade(conn)
	if err != nil {
		return nil, err
	}

	reg := prometheus.NewRegistry()
	var mc message.Creator
	if !ret.useProto {
		mc, err = message.NewCreator(
			reg,
			true,
			"",
			10*time.Second,
		)
	} else {
		mc, err = message.NewCreatorWithProto(
			reg,
			true,
			"",
			10*time.Second,
		)
	}
	if err != nil {
		return nil, err
	}

	metrics, err := network_peer.NewMetrics(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
	)
	if err != nil {
		return nil, err
	}

	ipPort := ips.IPPort{
		IP:   net.IPv6zero,
		Port: 0,
	}
	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, 10*time.Second)
	if err != nil {
		return nil, err
	}

	pingMessage, err := mc.Ping()
	if err != nil {
		return nil, err
	}

	peer := network_peer.Start(
		&network_peer.Config{
			Metrics:             metrics,
			MessageCreator:      mc,
			Log:                 logging.NoLog{},
			InboundMsgThrottler: throttling.NewNoInboundThrottler(),
			Network: network_peer.NewTestNetwork(
				mc,
				ret.networkID,
				ipPort,
				version.CurrentApp,
				tlsCert.PrivateKey.(crypto.Signer),
				ids.Set{},
				100,
			),
			Router:               router,
			VersionCompatibility: version.GetCompatibility(ret.networkID),
			MySubnets:            ids.Set{},
			Beacons:              validators.NewSet(),
			NetworkID:            ret.networkID,
			PingFrequency:        constants.DefaultPingFrequency,
			PongTimeout:          constants.DefaultPingPongTimeout,
			MaxClockDifference:   time.Minute,
			ResourceTracker:      resourceTracker,
			PingMessage:          pingMessage,
		},
		conn,
		cert,
		peerID,
		network_peer.NewBlockingMessageQueue(
			metrics,
			logging.NoLog{},
			maxMessageToSend,
		),
	)
	return peer, peer.AwaitReady(ctx)
}
