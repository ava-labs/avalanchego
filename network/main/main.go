package main

import (
	"context"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"go.uber.org/zap"
)

// run with in memory db, and
// ./build/avalanchego --network-id=local --db-type=memdb --sybil-protection-enabled=false
var (
	NetworkId = constants.LocalID
	// p chain id. 
	ChainID = ids.FromStringOrPanic("11111111111111111111111111111111LpoYY")
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
	go log.RecoverAndPanic(func() {
		time.Sleep(15 * time.Second)
		network.StartClose()
	})

	exampleMsg, err := getStateSummaryMsg(log, ChainID)
	if err != nil {
		log.Fatal(
			"failed to create outbound msg",
			zap.Error(err),
		)
		return
	}
	log.Info("Created example message", zap.Any("op", exampleMsg.Op().String()))
	
	ctx := context.Background()

	// if local network we send to ourselves(since no peers will be returned)
	sendToSelf(ctx, log, network, handler, exampleMsg)
	sendToAllPeers(ctx, log, network, handler, exampleMsg)

	// Calling network.Dispatch() will block until a fatal error occurs or
	// network.StartClose() is called.
	err = network.Dispatch()
	log.Info(
		"network exited",
		zap.Error(err),
	)
}