// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

const (
	// We cache processed vertices where height = c * stripeDistance for c = {1,2,3...}
	// This forms a "stripe" of cached DAG vertices at height stripeDistance, 2*stripeDistance, etc.
	// This helps to limit the number of repeated DAG traversals performed
	stripeDistance = 2000
	stripeWidth    = 5
	cacheSize      = 100000

	// maxOutstandingGetAncestorsRequests is the maximum number of GetAncestors
	// sent but not yet responded to/failed
	maxOutstandingGetAncestorsRequests = 10
)

var _ common.BootstrapableEngine = (*bootstrapper)(nil)

func New(
	config Config,
	onFinished func(ctx context.Context, lastReqID uint32) error,
) (common.BootstrapableEngine, error) {
	b := &bootstrapper{
		Config: config,

		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Ctx.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Ctx.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Ctx.Log),
		PutHandler:                  common.NewNoOpPutHandler(config.Ctx.Log),
		QueryHandler:                common.NewNoOpQueryHandler(config.Ctx.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(config.Ctx.Log),
		AppHandler:                  config.VM,

		processedCache: &cache.LRU[ids.ID, struct{}]{Size: cacheSize},
		Fetcher: common.Fetcher{
			OnFinished: onFinished,
		},
	}
	return b, b.metrics.Initialize("bs", config.Ctx.AvalancheRegisterer)
}

// Note: To align with the Snowman invariant, it should be guaranteed the VM is
// not used until after the bootstrapper has been Started.
type bootstrapper struct {
	Config
	common.Halter

	// list of NoOpsHandler for messages dropped by bootstrapper
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler

	common.Fetcher
	metrics

	// IDs of vertices that we will send a GetAncestors request for once we are
	// not at the max number of outstanding requests
	needToFetch set.Set[ids.ID]

	// Contains IDs of vertices that have recently been processed
	processedCache *cache.LRU[ids.ID, struct{}]

	// Tracks the last requestID that was used in a request
	requestID uint32
}

func (b *bootstrapper) Context() *snow.ConsensusContext {
	return b.Ctx
}

func (b *bootstrapper) Clear(context.Context) error {
	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	if err := b.VtxBlocked.Clear(); err != nil {
		return err
	}
	if err := b.TxBlocked.Clear(); err != nil {
		return err
	}
	if err := b.VtxBlocked.Commit(); err != nil {
		return err
	}
	return b.TxBlocked.Commit()
}

// Ancestors handles the receipt of multiple containers. Should be received in
// response to a GetAncestors message to [nodeID] with request ID [requestID].
// Expects vtxs[0] to be the vertex requested in the corresponding GetAncestors.
func (b *bootstrapper) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxs [][]byte) error {
	lenVtxs := len(vtxs)
	if lenVtxs == 0 {
		b.Ctx.Log.Debug("Ancestors contains no vertices",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return b.GetAncestorsFailed(ctx, nodeID, requestID)
	}
	if lenVtxs > b.Config.AncestorsMaxContainersReceived {
		b.Ctx.Log.Debug("ignoring containers in Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("numIgnored", lenVtxs-b.Config.AncestorsMaxContainersReceived),
		)

		vtxs = vtxs[:b.Config.AncestorsMaxContainersReceived]
	}

	requestedVtxID, requested := b.OutstandingRequests.Remove(nodeID, requestID)
	vtx, err := b.Manager.ParseVtx(ctx, vtxs[0]) // first vertex should be the one we requested in GetAncestors request
	if err != nil {
		if !requested {
			b.Ctx.Log.Debug("failed to parse unrequested vertex",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
			return nil
		}
		if b.Ctx.Log.Enabled(logging.Verbo) {
			b.Ctx.Log.Verbo("failed to parse requested vertex",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Stringer("vtxID", requestedVtxID),
				zap.Binary("vtxBytes", vtxs[0]),
				zap.Error(err),
			)
		} else {
			b.Ctx.Log.Debug("failed to parse requested vertex",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Stringer("vtxID", requestedVtxID),
				zap.Error(err),
			)
		}
		return b.fetch(ctx, requestedVtxID)
	}

	vtxID := vtx.ID()
	// If the vertex is neither the requested vertex nor a needed vertex, return early and re-fetch if necessary
	if requested && requestedVtxID != vtxID {
		b.Ctx.Log.Debug("received incorrect vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("vtxID", vtxID),
		)
		return b.fetch(ctx, requestedVtxID)
	}
	if !requested && !b.OutstandingRequests.Contains(vtxID) && !b.needToFetch.Contains(vtxID) {
		b.Ctx.Log.Debug("received un-needed vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("vtxID", vtxID),
		)
		return nil
	}

	// Do not remove from outstanding requests if this did not answer a specific outstanding request
	// to ensure that real responses are not dropped in favor of potentially byzantine Ancestors messages that
	// could force the node to bootstrap 1 vertex at a time.
	b.needToFetch.Remove(vtxID)

	// All vertices added to [processVertices] have received transitive votes from the accepted frontier
	processVertices := make([]avalanche.Vertex, 1, len(vtxs)) // Process all of the valid vertices in this message
	processVertices[0] = vtx
	parents, err := vtx.Parents()
	if err != nil {
		return err
	}
	eligibleVertices := set.NewSet[ids.ID](len(parents))
	for _, parent := range parents {
		eligibleVertices.Add(parent.ID())
	}

	for _, vtxBytes := range vtxs[1:] { // Parse/persist all the vertices
		vtx, err := b.Manager.ParseVtx(ctx, vtxBytes) // Persists the vtx
		if err != nil {
			b.Ctx.Log.Debug("failed to parse vertex",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
			b.Ctx.Log.Debug("failed to parse vertex",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("vtxBytes", vtxBytes),
				zap.Error(err),
			)
			break
		}
		vtxID := vtx.ID()
		if !eligibleVertices.Contains(vtxID) {
			b.Ctx.Log.Debug("received vertex that should not have been included",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Stringer("vtxID", vtxID),
			)
			break
		}
		eligibleVertices.Remove(vtxID)
		parents, err := vtx.Parents()
		if err != nil {
			return err
		}
		for _, parent := range parents {
			eligibleVertices.Add(parent.ID())
		}
		processVertices = append(processVertices, vtx)
		b.needToFetch.Remove(vtxID) // No need to fetch this vertex since we have it now
	}

	return b.process(ctx, processVertices...)
}

func (b *bootstrapper) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	vtxID, ok := b.OutstandingRequests.Remove(nodeID, requestID)
	if !ok {
		b.Ctx.Log.Debug("skipping GetAncestorsFailed call",
			zap.String("reason", "no matching outstanding request"),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	// Send another request for the vertex
	return b.fetch(ctx, vtxID)
}

func (b *bootstrapper) Connected(
	ctx context.Context,
	nodeID ids.NodeID,
	nodeVersion *version.Application,
) error {
	if err := b.VM.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	return b.StartupTracker.Connected(ctx, nodeID, nodeVersion)
}

func (b *bootstrapper) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := b.VM.Disconnected(ctx, nodeID); err != nil {
		return err
	}

	return b.StartupTracker.Disconnected(ctx, nodeID)
}

func (*bootstrapper) Timeout(context.Context) error {
	return nil
}

func (*bootstrapper) Gossip(context.Context) error {
	return nil
}

func (b *bootstrapper) Shutdown(ctx context.Context) error {
	b.Ctx.Log.Info("shutting down bootstrapper")

	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	return b.VM.Shutdown(ctx)
}

func (*bootstrapper) Notify(context.Context, common.Message) error {
	return nil
}

func (b *bootstrapper) Start(ctx context.Context, startReqID uint32) error {
	b.Ctx.Log.Info("starting bootstrap")

	b.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.Bootstrapping,
	})
	if err := b.VM.SetState(ctx, snow.Bootstrapping); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	if err := b.VtxBlocked.SetParser(ctx, &vtxParser{
		log:         b.Ctx.Log,
		numAccepted: b.numAcceptedVts,
		numDropped:  b.numDroppedVts,
		manager:     b.Manager,
	}); err != nil {
		return err
	}

	if err := b.TxBlocked.SetParser(&txParser{
		log:         b.Ctx.Log,
		numAccepted: b.numAcceptedTxs,
		numDropped:  b.numDroppedTxs,
		vm:          b.VM,
	}); err != nil {
		return err
	}

	b.requestID = startReqID

	// If the network was already linearized, don't attempt to linearize it
	// again.
	linearized, err := b.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get linearization status: %w", err)
	}
	if linearized {
		return b.ForceAccepted(ctx, nil)
	}

	// If a stop vertex is well known, accept that.
	if b.Config.StopVertexID != ids.Empty {
		b.Ctx.Log.Info("using well known stop vertex",
			zap.Stringer("vtxID", b.Config.StopVertexID),
		)

		return b.ForceAccepted(ctx, []ids.ID{b.Config.StopVertexID})
	}

	// If a stop vertex isn't well known, treat the current state as the final
	// DAG state.
	//
	// Note: This is used to linearize networks that were created after the
	// linearization occurred.
	edge := b.Manager.Edge(ctx)
	stopVertex, err := b.Manager.BuildStopVtx(ctx, edge)
	if err != nil {
		return fmt.Errorf("failed to create stop vertex: %w", err)
	}
	if err := stopVertex.Accept(ctx); err != nil {
		return fmt.Errorf("failed to accept stop vertex: %w", err)
	}

	stopVertexID := stopVertex.ID()
	b.Ctx.Log.Info("generated stop vertex",
		zap.Stringer("vtxID", stopVertexID),
	)

	return b.ForceAccepted(ctx, nil)
}

func (b *bootstrapper) HealthCheck(ctx context.Context) (interface{}, error) {
	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	vmIntf, vmErr := b.VM.HealthCheck(ctx)
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

func (b *bootstrapper) GetVM() common.VM {
	return b.VM
}

// Add the vertices in [vtxIDs] to the set of vertices that we need to fetch,
// and then fetch vertices (and their ancestors) until either there are no more
// to fetch or we are at the maximum number of outstanding requests.
func (b *bootstrapper) fetch(ctx context.Context, vtxIDs ...ids.ID) error {
	b.needToFetch.Add(vtxIDs...)
	for b.needToFetch.Len() > 0 && b.OutstandingRequests.Len() < maxOutstandingGetAncestorsRequests {
		vtxID := b.needToFetch.CappedList(1)[0]
		b.needToFetch.Remove(vtxID)

		// Make sure we haven't already requested this vertex
		if b.OutstandingRequests.Contains(vtxID) {
			continue
		}

		// Make sure we don't already have this vertex
		if _, err := b.Manager.GetVtx(ctx, vtxID); err == nil {
			continue
		}

		validatorIDs, err := b.Config.Beacons.Sample(b.Ctx.SubnetID, 1) // validator to send request to
		if err != nil {
			return fmt.Errorf("dropping request for %s as there are no validators", vtxID)
		}
		validatorID := validatorIDs[0]
		b.requestID++

		b.OutstandingRequests.Add(validatorID, b.requestID, vtxID)
		b.Config.Sender.SendGetAncestors(ctx, validatorID, b.requestID, vtxID) // request vertex and ancestors
	}
	return b.checkFinish(ctx)
}

// Process the vertices in [vtxs].
func (b *bootstrapper) process(ctx context.Context, vtxs ...avalanche.Vertex) error {
	// Vertices that we need to process prioritized by vertices that are unknown
	// or the furthest down the DAG. Unknown vertices are prioritized to ensure
	// that once we have made it below a certain height in DAG traversal we do
	// not need to reset and repeat DAG traversals.
	toProcess := heap.NewMap[ids.ID, avalanche.Vertex](vertexLess)
	for _, vtx := range vtxs {
		vtxID := vtx.ID()
		if _, ok := b.processedCache.Get(vtxID); !ok { // only process a vertex if we haven't already
			_, _ = toProcess.Push(vtxID, vtx)
		} else {
			b.VtxBlocked.RemoveMissingID(vtxID)
		}
	}

	vtxHeightSet := set.Set[ids.ID]{}
	prevHeight := uint64(0)

	for {
		if b.Halted() {
			return nil
		}

		vtxID, vtx, ok := toProcess.Pop()
		if !ok {
			break
		}

		switch vtx.Status() {
		case choices.Unknown:
			b.VtxBlocked.AddMissingID(vtxID)
			b.needToFetch.Add(vtxID) // We don't have this vertex locally. Mark that we need to fetch it.
		case choices.Rejected:
			return fmt.Errorf("tried to accept %s even though it was previously rejected", vtxID)
		case choices.Processing:
			b.needToFetch.Remove(vtxID)
			b.VtxBlocked.RemoveMissingID(vtxID)

			// Add to queue of vertices to execute when bootstrapping finishes.
			pushed, err := b.VtxBlocked.Push(ctx, &vertexJob{
				log:         b.Ctx.Log,
				numAccepted: b.numAcceptedVts,
				numDropped:  b.numDroppedVts,
				vtx:         vtx,
			})
			if err != nil {
				return err
			}
			if !pushed {
				// If the vertex is already on the queue, then we have already
				// pushed [vtx]'s transactions and traversed into its parents.
				continue
			}

			txs, err := vtx.Txs(ctx)
			if err != nil {
				return err
			}

			for _, tx := range txs {
				// Add to queue of txs to execute when bootstrapping finishes.
				pushed, err := b.TxBlocked.Push(ctx, &txJob{
					log:         b.Ctx.Log,
					numAccepted: b.numAcceptedTxs,
					numDropped:  b.numDroppedTxs,
					tx:          tx,
				})
				if err != nil {
					return err
				}
				if pushed {
					b.numFetchedTxs.Inc()
				}
			}

			b.numFetchedVts.Inc()

			verticesFetchedSoFar := b.VtxBlocked.Jobs.PendingJobs()
			if verticesFetchedSoFar%common.StatusUpdateFrequency == 0 { // Periodically print progress
				b.Ctx.Log.Info("fetched vertices",
					zap.Uint64("numVerticesFetched", verticesFetchedSoFar),
				)
			}

			parents, err := vtx.Parents()
			if err != nil {
				return err
			}
			for _, parent := range parents { // Process the parents of this vertex (traverse up the DAG)
				parentID := parent.ID()
				if _, ok := b.processedCache.Get(parentID); !ok { // But only if we haven't processed the parent
					if !vtxHeightSet.Contains(parentID) {
						toProcess.Push(parentID, parent)
					}
				}
			}
			height, err := vtx.Height()
			if err != nil {
				return err
			}
			if height%stripeDistance < stripeWidth { // See comment for stripeDistance
				b.processedCache.Put(vtxID, struct{}{})
			}
			if height == prevHeight {
				vtxHeightSet.Add(vtxID)
			} else {
				// Set new height and reset [vtxHeightSet]
				prevHeight = height
				vtxHeightSet.Clear()
				vtxHeightSet.Add(vtxID)
			}
		}
	}

	if err := b.TxBlocked.Commit(); err != nil {
		return err
	}
	if err := b.VtxBlocked.Commit(); err != nil {
		return err
	}

	return b.fetch(ctx)
}

// ForceAccepted starts bootstrapping. Process the vertices in [accepterContainerIDs].
func (b *bootstrapper) ForceAccepted(ctx context.Context, acceptedContainerIDs []ids.ID) error {
	pendingContainerIDs := b.VtxBlocked.MissingIDs()
	// Append the list of accepted container IDs to pendingContainerIDs to ensure
	// we iterate over every container that must be traversed.
	pendingContainerIDs = append(pendingContainerIDs, acceptedContainerIDs...)
	b.Ctx.Log.Debug("starting bootstrapping",
		zap.Int("numMissingVertices", len(pendingContainerIDs)),
		zap.Int("numAcceptedVertices", len(acceptedContainerIDs)),
	)
	toProcess := make([]avalanche.Vertex, 0, len(pendingContainerIDs))
	for _, vtxID := range pendingContainerIDs {
		if vtx, err := b.Manager.GetVtx(ctx, vtxID); err == nil {
			if vtx.Status() == choices.Accepted {
				b.VtxBlocked.RemoveMissingID(vtxID)
			} else {
				toProcess = append(toProcess, vtx) // Process this vertex.
			}
		} else {
			b.VtxBlocked.AddMissingID(vtxID)
			b.needToFetch.Add(vtxID) // We don't have this vertex. Mark that we have to fetch it.
		}
	}
	return b.process(ctx, toProcess...)
}

// checkFinish repeatedly executes pending transactions and requests new frontier blocks until there aren't any new ones
// after which it finishes the bootstrap process
func (b *bootstrapper) checkFinish(ctx context.Context) error {
	// If we still need to fetch vertices, we can't finish
	if len(b.VtxBlocked.MissingIDs()) > 0 {
		return nil
	}

	b.Ctx.Log.Info("executing transactions")
	_, err := b.TxBlocked.ExecuteAll(
		ctx,
		b.Config.Ctx,
		b,
		false,
		b.Ctx.TxAcceptor,
	)
	if err != nil || b.Halted() {
		return err
	}

	b.Ctx.Log.Info("executing vertices")
	_, err = b.VtxBlocked.ExecuteAll(
		ctx,
		b.Config.Ctx,
		b,
		false,
		b.Ctx.VertexAcceptor,
	)
	if err != nil || b.Halted() {
		return err
	}

	// Invariant: edge will only be the stop vertex
	edge := b.Manager.Edge(ctx)
	stopVertexID := edge[0]
	if err := b.VM.Linearize(ctx, stopVertexID); err != nil {
		return err
	}

	b.processedCache.Flush()
	return b.OnFinished(ctx, b.requestID)
}

// A vertex is less than another vertex if it is unknown. Ties are broken by
// prioritizing vertices that have a greater height.
func vertexLess(i, j avalanche.Vertex) bool {
	if !i.Status().Fetched() {
		return true
	}
	if !j.Status().Fetched() {
		return false
	}

	// Treat errors on retrieving the height as if the vertex is not fetched
	heightI, errI := i.Height()
	if errI != nil {
		return true
	}
	heightJ, errJ := j.Height()
	if errJ != nil {
		return false
	}

	return heightI > heightJ
}
