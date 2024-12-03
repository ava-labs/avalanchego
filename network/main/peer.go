package main

import (
	"context"
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

type TestPeers struct {
	log   logging.Logger
	peers []peer.Peer
}

func NewTestPeers(ctx context.Context, log logging.Logger, network network.Network, handler *testExternalHandler, bootstrappers []genesis.Bootstrapper) (*TestPeers, error) {
	if NetworkId == constants.LocalID {
		p, err := peer.StartTestPeer(
			ctx,
			netip.MustParseAddrPort(LocalIP),
			constants.LocalID,
			handler,
		)

		if err != nil {
			return nil, err
		}
		return &TestPeers{
			log:   log,
			peers: []peer.Peer{p},
		}, nil
	}

	// adds peers to the network
	log.Info("Connect to bootstrappers")
	bootstrappers = trackBootstrappers(network, bootstrappers)
	time.Sleep(8 * time.Second)

	// grab peer info
	// peerInfo := network.PeerInfo(nil)

	var peers []peer.Peer
	for _, info := range bootstrappers {
		p, err := peer.StartTestPeer(
			ctx,
			info.IP,
			NetworkId,
			handler,
		)
		if err != nil {
			// continue in case of failure but note in log
			log.Fatal(
				"failed to create test peer",
				zap.String("ID", info.ID.String()),
				zap.Error(err),
			)
			continue
		} else {
			peers = append(peers, p)
		}
	}

	log.Info("Successfully connected ", zap.Int("num connected", len(peers)), zap.Int("num total", len(bootstrappers)))

	return &TestPeers{
		log,
		peers,
	}, nil
}

func (t TestPeers) Send(ctx context.Context, msg message.OutboundMessage) int {
	success := 0
	for _, p := range t.peers {
		if p.Send(ctx, msg) {
			success++
			t.log.Info("Successfully sent message to peer")
		} else {
			t.log.Info("Message not delivered to peer")
		}
		p.Info()
	}
	return success
}
