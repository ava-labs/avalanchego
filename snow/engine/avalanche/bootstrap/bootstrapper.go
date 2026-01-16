// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/bimap"
	"github.com/ava-labs/avalanchego/utils/heap"
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

	// statusUpdateFrequency is how many containers should be processed between
	// logs
	statusUpdateFrequency = 5000

	// maxOutstandingGetAncestorsRequests is the maximum number of GetAncestors
	// sent but not yet responded to/failed
	maxOutstandingGetAncestorsRequests = 10

	epsilon = 1e-6 // small amount to add to time to avoid division by 0
)

var _ common.BootstrapableEngine = (*Bootstrapper)(nil)

func New(
	config Config,
	onFinished func(ctx context.Context, lastReqID uint32) error,
	reg prometheus.Registerer,
) (*Bootstrapper, error) {
	b := &Bootstrapper{
		Config: config,

		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Ctx.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Ctx.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Ctx.Log),
		PutHandler:                  common.NewNoOpPutHandler(config.Ctx.Log),
		QueryHandler:                common.NewNoOpQueryHandler(config.Ctx.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(config.Ctx.Log),
		SimplexHandler:              common.NewNoOpSimplexHandler(config.Ctx.Log),
		AppHandler:                  config.VM,

		outstandingRequests:     bimap.New[common.Request, ids.ID](),
		outstandingRequestTimes: make(map[common.Request]time.Time),

		processedCache: lru.NewCache[ids.ID, struct{}](cacheSize),
		onFinished:     onFinished,
	}
	return b, b.metrics.Initialize(reg)
}

// Note: To align with the Snowman invariant, it should be guaranteed the VM is
// not used until after the Bootstrapper has been Started.
type Bootstrapper struct {
	Config

	// list of NoOpsHandler for messages dropped by Bootstrapper
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler
	common.SimplexHandler

	metrics

	// tracks which validators were asked for which containers in which requests
	outstandingRequests     *bimap.BiMap[common.Request, ids.ID]
	outstandingRequestTimes map[common.Request]time.Time

	// IDs of vertices that we will send a GetAncestors request for once we are
	// not at the max number of outstanding requests
	needToFetch set.Set[ids.ID]

	// Contains IDs of vertices that have recently been processed
	processedCache *lru.Cache[ids.ID, struct{}]

	// Tracks the last requestID that was used in a request
	requestID uint32

	// Called when bootstrapping is done on a specific chain
	onFinished func(ctx context.Context, lastReqID uint32) error
}

func (b *Bootstrapper) Context() *snow.ConsensusContext {
	return b.Ctx
}

func (b *Bootstrapper) Clear(context.Context) error {
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
func (b *Bootstrapper) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxs [][]byte) error {
	request := common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	}
	requestedVtxID, ok := b.outstandingRequests.DeleteKey(request)
	if !ok { // this message isn't in response to a request we made
		b.Ctx.Log.Debug("received unexpected Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	requestTime := b.outstandingRequestTimes[request]
	delete(b.outstandingRequestTimes, request)

	lenVtxs := len(vtxs)
	if lenVtxs == 0 {
		b.Ctx.Log.Debug("Ancestors contains no vertices",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)

		b.PeerTracker.RegisterFailure(nodeID)
		return b.fetch(ctx, requestedVtxID)
	}

	if lenVtxs > b.Config.AncestorsMaxContainersReceived {
		vtxs = vtxs[:b.Config.AncestorsMaxContainersReceived]

		b.Ctx.Log.Debug("ignoring containers in Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("numIgnored", lenVtxs-b.Config.AncestorsMaxContainersReceived),
		)
	}

	vtx, err := b.Manager.ParseVtx(ctx, vtxs[0])
	if err != nil {
		b.Ctx.Log.Debug("failed to parse requested vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("vtxID", requestedVtxID),
			zap.Error(err),
		)

		b.PeerTracker.RegisterFailure(nodeID)
		return b.fetch(ctx, requestedVtxID)
	}

	if actualID := vtx.ID(); actualID != requestedVtxID {
		b.Ctx.Log.Debug("received incorrect vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("vtxID", actualID),
		)

		b.PeerTracker.RegisterFailure(nodeID)
		return b.fetch(ctx, requestedVtxID)
	}

	b.needToFetch.Remove(requestedVtxID)

	// All vertices added to [verticesToProcess] have received transitive votes
	// from the accepted frontier.
	var (
		numBytes          = len(vtxs[0])
		verticesToProcess = make([]avalanche.Vertex, 1, len(vtxs))
	)
	verticesToProcess[0] = vtx

	parents, err := vtx.Parents()
	if err != nil {
		return err
	}
	eligibleVertices := set.NewSet[ids.ID](len(parents))
	for _, parent := range parents {
		eligibleVertices.Add(parent.ID())
	}

	for _, vtxBytes := range vtxs[1:] {
		vtx, err := b.Manager.ParseVtx(ctx, vtxBytes) // Persists the vtx
		if err != nil {
			b.Ctx.Log.Debug("failed to parse vertex",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
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

		numBytes += len(vtxBytes)
		verticesToProcess = append(verticesToProcess, vtx)
		b.needToFetch.Remove(vtxID) // No need to fetch this vertex since we have it now
	}

	// TODO: Calculate bandwidth based on the vertices that were persisted to
	// disk.
	var (
		requestLatency = time.Since(requestTime).Seconds() + epsilon
		bandwidth      = float64(numBytes) / requestLatency
	)
	b.PeerTracker.RegisterResponse(nodeID, bandwidth)

	return b.process(ctx, verticesToProcess...)
}

func (b *Bootstrapper) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	request := common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	}
	vtxID, ok := b.outstandingRequests.DeleteKey(request)
	if !ok {
		b.Ctx.Log.Debug("unexpectedly called GetAncestorsFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	delete(b.outstandingRequestTimes, request)

	// This node timed out their request.
	b.PeerTracker.RegisterFailure(nodeID)

	// Send another request for the vertex
	return b.fetch(ctx, vtxID)
}

func (b *Bootstrapper) Connected(
	ctx context.Context,
	nodeID ids.NodeID,
	nodeVersion *version.Application,
) error {
	if err := b.VM.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	return b.StartupTracker.Connected(ctx, nodeID, nodeVersion)
}

func (b *Bootstrapper) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := b.VM.Disconnected(ctx, nodeID); err != nil {
		return err
	}

	return b.StartupTracker.Disconnected(ctx, nodeID)
}

func (*Bootstrapper) Timeout(context.Context) error {
	return nil
}

func (*Bootstrapper) Gossip(context.Context) error {
	return nil
}

func (b *Bootstrapper) Shutdown(ctx context.Context) error {
	b.Ctx.Log.Info("shutting down Bootstrapper")

	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	return b.VM.Shutdown(ctx)
}

func (*Bootstrapper) Notify(context.Context, common.Message) error {
	return nil
}

func (b *Bootstrapper) Start(ctx context.Context, startReqID uint32) error {
	b.Ctx.Log.Info("starting bootstrap")

	b.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_DAG,
		State: snow.Bootstrapping,
	})
	if err := b.VM.SetState(ctx, snow.Bootstrapping); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	if err := b.VtxBlocked.SetParser(ctx, &vtxParser{
		log:         b.Ctx.Log,
		numAccepted: b.numAcceptedVts,
		manager:     b.Manager,
	}); err != nil {
		return err
	}

	if err := b.TxBlocked.SetParser(&txParser{
		log:         b.Ctx.Log,
		numAccepted: b.numAcceptedTxs,
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
		return b.startSyncing(ctx, nil)
	}

	// If a stop vertex is well known, accept that.
	if b.Config.StopVertexID != ids.Empty {
		b.Ctx.Log.Info("using well known stop vertex",
			zap.Stringer("vtxID", b.Config.StopVertexID),
		)

		return b.startSyncing(ctx, []ids.ID{b.Config.StopVertexID})
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

	return b.startSyncing(ctx, nil)
}

func (b *Bootstrapper) HealthCheck(ctx context.Context) (interface{}, error) {
	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	vmIntf, vmErr := b.VM.HealthCheck(ctx)
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

// Add the vertices in [vtxIDs] to the set of vertices that we need to fetch,
// and then fetch vertices (and their ancestors) until either there are no more
// to fetch or we are at the maximum number of outstanding requests.
func (b *Bootstrapper) fetch(ctx context.Context, vtxIDs ...ids.ID) error {
	b.needToFetch.Add(vtxIDs...)
	for b.needToFetch.Len() > 0 && b.outstandingRequests.Len() < maxOutstandingGetAncestorsRequests {
		vtxID, _ := b.needToFetch.Pop() // Length checked in predicate above

		// Make sure we haven't already requested this vertex
		if b.outstandingRequests.HasValue(vtxID) {
			continue
		}

		// Make sure we don't already have this vertex
		if _, err := b.Manager.GetVtx(ctx, vtxID); err == nil {
			continue
		}

		nodeID, ok := b.PeerTracker.SelectPeer()
		if !ok {
			// If we aren't connected to any peers, we send a request to ourself
			// which is guaranteed to fail. We send this message to use the
			// message timeout as a retry mechanism. Once we are connected to
			// another node again we will select them to sample from.
			nodeID = b.Ctx.NodeID
		}

		b.PeerTracker.RegisterRequest(nodeID)

		b.requestID++
		request := common.Request{
			NodeID:    nodeID,
			RequestID: b.requestID,
		}
		b.outstandingRequests.Put(request, vtxID)
		b.outstandingRequestTimes[request] = time.Now()
		b.Config.Sender.SendGetAncestors(ctx, nodeID, b.requestID, vtxID) // request vertex and ancestors
	}
	return b.checkFinish(ctx)
}

// Process the vertices in [vtxs].
func (b *Bootstrapper) process(ctx context.Context, vtxs ...avalanche.Vertex) error {
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
			if verticesFetchedSoFar%statusUpdateFrequency == 0 { // Periodically print progress
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

// startSyncing starts bootstrapping. Process the vertices in [accepterContainerIDs].
func (b *Bootstrapper) startSyncing(ctx context.Context, acceptedContainerIDs []ids.ID) error {
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
func (b *Bootstrapper) checkFinish(ctx context.Context) error {
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
	return b.onFinished(ctx, b.requestID)
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
