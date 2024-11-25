package main

import (
	"context"
	"net/netip"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

func sendToAllPeers(log logging.Logger, network network.Network, handler *testExternalHandler, msg message.OutboundMessage) {
	// grab peer info
	peerInfo := network.PeerInfo([]ids.NodeID{})
	log.Info("Peer Info", zap.Any("peers", peerInfo))

	ctx := context.Background()
	for _, info := range peerInfo {
		peer, err := peer.StartTestPeer(
			ctx,
			info.IP,
			NetworkId,
			handler,
		)
	
		if err != nil {
			log.Fatal(
				"failed to create test peer",
				zap.Error(err),
			)
			// return
		} else {
			sent := peer.Send(ctx, msg)
			log.Info("Message sent to peer", zap.Any("sent", sent))
		}
	}
}

func sendToSelf(ctx context.Context, log logging.Logger, network network.Network, handler *testExternalHandler, msg message.OutboundMessage) {
	peer, err := peer.StartTestPeer(
				ctx,
				netip.MustParseAddrPort("127.0.0.1:9651"),
				constants.LocalID,
				handler,
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