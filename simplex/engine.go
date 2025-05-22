package simplex

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var _ common.Engine = (*Engine)(nil)

type Engine struct {
}

func (e *Engine) Simplex(nodeID ids.NodeID, msg *p2p.Simplex) error {
	fmt.Println("Simplex message received in the simplex handler!")
	return nil
}

/* no-op methods for the Engine interface */
func (e *Engine) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}

func (e *Engine) GetStateSummaryFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs set.Set[ids.ID]) error {
	return nil
}

func (e *Engine) GetAcceptedStateSummaryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

func (e *Engine) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	return nil
}

func (e *Engine) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error {
	return nil
}

func (e *Engine) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}

func (e *Engine) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64) error {
	return nil
}

func (e *Engine) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte, requestedHeight uint64) error {
	return nil
}

func (e *Engine) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) error {
	return nil
}

func (e *Engine) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return nil
}

func (e *Engine) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

func (e *Engine) Gossip(ctx context.Context) error {
	return nil
}

func (e *Engine) Shutdown(ctx context.Context) error {
	return nil
}

func (e *Engine) Notify(ctx context.Context, msg common.Message) error {
	return nil
}

func (e *Engine) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}

func (e *Engine) GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights set.Set[uint64]) error {
	return nil
}

func (e *Engine) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	return nil
}

func (e *Engine) GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (e *Engine) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

func (e *Engine) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

func (e *Engine) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return nil
}

func (e *Engine) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return nil
}

func (e *Engine) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	return nil
}

func (e *Engine) HealthCheck(ctx context.Context) (interface{}, error) {
	return nil, nil
}

func (e *Engine) Start(ctx context.Context, startReqID uint32) error {
	return nil
}
