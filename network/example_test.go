// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var _ router.ExternalHandler = (*testExternalHandler)(nil)

// Note: all of the external handler's methods are called on peer goroutines. It
// is possible for multiple concurrent calls to happen with different NodeIDs.
// However, a given NodeID will only be performing one call at a time.
type testExternalHandler struct {
	log logging.Logger
}

// Note: HandleInbound will be called with raw P2P messages, the networking
// implementation does not implicitly register timeouts, so this handler is only
// called by messages explicitly sent by the peer. If timeouts are required,
// that must be handled by the user of this utility.
func (t *testExternalHandler) HandleInbound(_ context.Context, message message.InboundMessage) {
	t.log.Info(
		"receiving message",
		zap.Stringer("op", message.Op()),
	)
}

func (t *testExternalHandler) Connected(nodeID ids.NodeID, version *version.Application, subnetID ids.ID) {
	t.log.Info(
		"connected",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("version", version),
		zap.Stringer("subnetID", subnetID),
	)
}

func (t *testExternalHandler) Disconnected(nodeID ids.NodeID) {
	t.log.Info(
		"disconnected",
		zap.Stringer("nodeID", nodeID),
	)
}

type testAggressiveValidatorManager struct {
	validators.Manager
}

func (*testAggressiveValidatorManager) Contains(ids.ID, ids.NodeID) bool {
	return true
}

func ExampleNewTestNetwork() {
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

	// If we want to be able to communicate with non-primary network subnets, we
	// should register them here.
	trackedSubnets := set.Set[ids.ID]{}

	// Messages and connections are handled by the external handler.
	handler := &testExternalHandler{
		log: log,
	}
	metrics := prometheus.NewRegistry()
	cfg, err := NewTestNetworkConfig(
		metrics,
		constants.FujiID,
		validators,
		trackedSubnets,
	)
	if err != nil {
		log.Fatal(
			"failed to create test network config",
			zap.Error(err),
		)
		return
	}
	network, err := NewTestNetwork(
		log,
		metrics,
		cfg,
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
	bootstrappers := genesis.SampleBootstrappers(constants.FujiID, 5)
	for _, bootstrapper := range bootstrappers {
		network.ManuallyTrack(
			context.Background(),
			bootstrapper.ID,
			bootstrapper.IP,
		)
	}

	// Typically network.StartClose() should be called based on receiving a
	// SIGINT or SIGTERM. For the example, we close the network after 15s.
	go log.RecoverAndPanic(func() {
		time.Sleep(15 * time.Second)
		network.StartClose()
	})

	// network.Send(...) and network.Gossip(...) can be used here to send
	// messages to peers.

	// Calling network.Dispatch() will block until a fatal error occurs or
	// network.StartClose() is called.
	err = network.Dispatch(context.Background())
	log.Info(
		"network exited",
		zap.Error(err),
	)
}
