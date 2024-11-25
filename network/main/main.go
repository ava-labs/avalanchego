package main

import (
	"os"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"go.uber.org/zap"
)

type testAggressiveValidatorManager struct {
	validators.Manager
}

func main() {
	log := logging.NewLogger(
		"networking",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.Colors.ConsoleEncoder(),
		),
	)
	log.Info("current comp", zap.Any("comp", version.CurrentApp))
	log.Info("current comp", zap.Any("all", version.Current))
	bootstrappers, err := getBootstrappers(log)
	if err != nil {
		log.Fatal(
			"failed to get bootstrappers",
			zap.Error(err),
		)
		return
	}
	log.Info("bootstrappers", zap.Any("bootstrappers", bootstrappers))

	// Needs to be periodically updated by the caller to have the latest
	// validator set
	validators := &testAggressiveValidatorManager{
		Manager: validators.NewManager(),
	}

	// Messages and connections are handled by the external handler.
	handler := &testExternalHandler{
		log: log,
	}

	network, err := network.NewTestNetwork(
		log,
		constants.FujiID,
		validators,
		set.Set[ids.ID]{},
		handler,
	)
	
	if err != nil {
		log.Fatal(
			"failed to create test network",
			zap.Error(err),
		)
		return
	}
	
	// We need to initially connect to some nodes in the network before peer
	// gossip will enable connecting to all the remaining nodes in the network.
	bootstrappers = genesis.SampleBootstrappers(constants.FujiID, 5)
	var nodeIds set.Set[ids.NodeID]
	for _, bootstrapper := range bootstrappers {
		nodeIds.Add(bootstrapper.ID)
		network.ManuallyTrack(bootstrapper.ID, bootstrapper.IP)
		log.Info("manually tracking bootstrapper", zap.Any("bootstrapper", bootstrapper))
	}	
	time.Sleep(5 * time.Second)

	// Typically network.StartClose() should be called based on receiving a
	// SIGINT or SIGTERM. For the example, we close the network after 15s.
	// go log.RecoverAndPanic(func() {
	// 	time.Sleep(15 * time.Second)
	// 	network.StartClose()
	// })

	// grab peer info
	peerInfo := network.PeerInfo(nodeIds.List())
	log.Info("Peer Info", zap.Any("peers", peerInfo))

	sendPingMsg(log, network, nodeIds)
	

	// ctx := context.Background()

	// for _, info := range peerInfo {
	// 	peer, err := peer.StartTestPeer(
	// 		ctx,
	// 		info.IP,
	// 		constants.FujiID,
	// 		handler,
	// 	)
	
	// 	if err != nil {
	// 		log.Fatal(
	// 			"failed to create test peer",
	// 			zap.Error(err),
	// 		)
	// 		// return
	// 	} else {
	// 			// Send messages here with [peer.Send].
	// 		msgResp := peer.Send(ctx, &pingMessage{
	// 			Msg: "ping",
	// 		})
	// 		log.Info("message response", zap.Any("msgResp", msgResp))
	// 		// .Info()
	// 		// log.Info("peer info", zap.Any("peerInfo", info))
	// 	}
	// }

	// Calling network.Dispatch() will block until a fatal error occurs or
	// network.StartClose() is called.
	err = network.Dispatch()
	log.Info(
		"network exited",
		zap.Error(err),
	)
}