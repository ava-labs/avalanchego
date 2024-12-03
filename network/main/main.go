package main

import (
	"context"
	"os"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"go.uber.org/zap"
)

// For a local network with an in memory db run
// ./build/avalanchego --network-id=local --db-type=memdb --sybil-protection-enabled=false
var (
	NetworkId = constants.FujiID
	// p chain id.
	ChainID = ids.FromStringOrPanic("11111111111111111111111111111111LpoYY")
	LocalIP = "127.0.0.1:9651"
)

type testAggressiveValidatorManager struct {
	validators.Manager
}

func main() {
	ctx := context.Background()
	log := logging.NewLogger(
		"networking",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.Colors.ConsoleEncoder(),
		),
	)

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
		NetworkId,
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

	// Typically network.StartClose() should be called based on receiving a
	// SIGINT or SIGTERM. For the example, we close the network after 15s.
	// go log.RecoverAndPanic(func() {
	// 	time.Sleep(15 * time.Second)
	// 	network.StartClose()
	// })
	// nodeID, err := ids.NodeIDFromString("NodeID-CduWdu3Gv7bsqAxnsdTCWuuMEyLYQqchX")
	// if err != nil {
	// 	log.Fatal("node Id not correct")
	// }
	// bootstrappers := []genesis.Bootstrapper{
	// 	genesis.Bootstrapper{
	// 		ID: nodeID,

	// 	}
	// }
	tp, err := NewTestPeers(ctx, log, network, handler, []genesis.Bootstrapper{})
	if err != nil {
		log.Fatal("failed to get and start peers",
			zap.Error(err),
		)
	}

	// exampleMsg, err := getStateSummaryMsg(log, ChainID)
	// if err != nil {
	// 	log.Fatal(
	// 		"failed to create outbound msg",
	// 		zap.Error(err),
	// 	)
	// 	return
	// }
	// tp.Send(ctx, exampleMsg)

	metrics := newMetrics(log)
	go metrics.collect(tp)

	// Calling network.Dispatch() will block until a fatal error occurs or
	// network.StartClose() is called.
	err = network.Dispatch()
	log.Info(
		"network exited",
		zap.Error(err),
	)
}
