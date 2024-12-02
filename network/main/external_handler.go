package main

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"go.uber.org/zap"
)


var _ router.ExternalHandler = (*testExternalHandler)(nil)

// Note: all of the external handler's methods are called on peer goroutines. It
// is possible for multiple concurrent calls to happen with different NodeIDs.
// However, a given NodeID will only be performing one call at a time.
type testExternalHandler struct {
	log logging.Logger
	con bool
}

// Note: HandleInbound will be called with raw P2P messages, the networking
// implementation does not implicitly register timeouts, so this handler is only
// called by messages explicitly sent by the peer. If timeouts are required,
// that must be handled by the user of this utility.
func (t *testExternalHandler) HandleInbound(_ context.Context, msg message.InboundMessage) {
	// select on the message's operation
	switch msg.Op() {
	case message.Op(message.PullQueryOp):
		if !t.con {
			t.log.Info("received pull query")
			t.con = true
		}
		return
	case message.Op(message.PingOp): 
		t.log.Info("PINGGGO")
		return
	case message.Op(message.PongOp): 
		t.log.Info("Pongo was his name-o")
		return
	default:
		t.log.Info("received message", zap.Stringer("op", msg.Op()))
	}
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
