package main

import (
	"context"
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

func getAllPeers(ctx context.Context, log logging.Logger, network network.Network, handler *testExternalHandler) ([]peer.Peer, error) {
	if NetworkId == constants.LocalID {
		return []peer.Peer{}, nil
	}

	// adds peers to the network
	trackBootstrappers(network)

	time.Sleep(8 * time.Second)

	// grab peer info
	peerInfo := network.PeerInfo(nil)
	log.Info("Peer Info", zap.Any("peers", peerInfo))
	
	var peers []peer.Peer
	connected := 0
	for _, info := range peerInfo {
		p, err := peer.StartTestPeer(
			ctx,
			info.IP,
			NetworkId,
			handler,
			true,
		)
		
		if err != nil {
			// continue in case of failure but note in log
			log.Fatal(
				"failed to create test peer",
				zap.Error(err),
			)
			continue
		} else {
			connected++
			peers = append(peers, p)
		}
	}

	log.Info("Successfully connected ", zap.Int("num connected", connected), zap.Int("num total", len(peerInfo)))

	return peers, nil
}

func sendToSelf(ctx context.Context, log logging.Logger, network network.Network, handler *testExternalHandler, msg message.OutboundMessage) {
	peer, err := peer.StartTestPeer(
				ctx,
				netip.MustParseAddrPort("127.0.0.1:9651"),
				constants.LocalID,
				handler,
				true,
			)
	if err != nil {
		log.Fatal(
			"failed to create subnet ID",
			zap.Error(err),
		)
		return
	}

	sent := peer.Send(ctx, msg)
	log.Info("Sent msg", zap.Bool("sent", sent))
}


func send(ctx context.Context, log logging.Logger, peers []peer.Peer, msg message.OutboundMessage) int {
	success := 0
	for _, p := range peers {
		if p.Send(ctx, msg) {
			success++
			log.Info("Successfully sent message to peer")
		} else {
			log.Info("Message not delivered to peer")
		}
	}
	return success
}