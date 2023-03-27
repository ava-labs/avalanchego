// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche/poll"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/events"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

var _ Engine = (*Transitive)(nil)

func New(
	config Config,
	startSnowmanConsensus func(ctx context.Context, lastReqID uint32) error,
) (Engine, error) {
	return newTransitive(config, startSnowmanConsensus)
}

// Transitive implements the Engine interface by attempting to fetch all
// transitive dependencies.
type Transitive struct {
	Config
	metrics

	// list of NoOpsHandler for messages dropped by engine
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.AppHandler
	validators.Connector

	RequestID uint32

	// acceptedFrontiers of the other validators of this chain
	acceptedFrontiers tracker.Accepted

	polls poll.Set // track people I have asked for their preference

	// The set of vertices that have been requested in Get messages but not yet received
	outstandingVtxReqs common.Requests

	// missingTxs tracks transaction that are missing
	missingTxs set.Set[ids.ID]

	// IDs of vertices that are queued to be added to consensus but haven't yet been
	// because of missing dependencies
	pending set.Set[ids.ID]

	// vtxBlocked tracks operations that are blocked on vertices
	// txBlocked tracks operations that are blocked on transactions
	vtxBlocked, txBlocked events.Blocker

	// transactions that have been provided from the VM but that are pending to
	// be issued once the number of processing vertices has gone below the
	// optimal number.
	pendingTxs []snowstorm.Tx

	startSnowmanConsensus func(ctx context.Context, lastReqID uint32) error

	// A uniform sampler without replacement
	uniformSampler sampler.Uniform

	errs wrappers.Errs
}

func newTransitive(
	config Config,
	startSnowmanConsensus func(ctx context.Context, lastReqID uint32) error,
) (*Transitive, error) {
	config.Ctx.Log.Info("initializing consensus engine")

	acceptedFrontiers := tracker.NewAccepted()
	config.Validators.RegisterCallbackListener(acceptedFrontiers)

	factory := poll.NewEarlyTermNoTraversalFactory(config.Params.Alpha)

	t := &Transitive{
		Config:                      config,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Ctx.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Ctx.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Ctx.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(config.Ctx.Log),
		AppHandler:                  config.VM,
		Connector:                   config.VM,
		acceptedFrontiers:           acceptedFrontiers,
		polls: poll.NewSet(factory,
			config.Ctx.Log,
			"",
			config.Ctx.AvalancheRegisterer,
		),
		startSnowmanConsensus: startSnowmanConsensus,
		uniformSampler:        sampler.NewUniform(),
	}

	return t, t.metrics.Initialize("", config.Ctx.AvalancheRegisterer)
}

func (t *Transitive) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxBytes []byte) error {
	t.Ctx.Log.Verbo("called Put",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)

	// If the chain is linearized, we should immediately drop all put messages.
	linearized, err := t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return err
	}
	if linearized {
		return nil
	}

	vtx, err := t.Manager.ParseVtx(ctx, vtxBytes)
	if err != nil {
		t.Ctx.Log.Debug("failed to parse vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		t.Ctx.Log.Verbo("failed to parse vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Binary("vertex", vtxBytes),
			zap.Error(err),
		)
		return t.GetFailed(ctx, nodeID, requestID)
	}

	actualVtxID := vtx.ID()
	expectedVtxID, ok := t.outstandingVtxReqs.Get(nodeID, requestID)
	// If the provided vertex is not the requested vertex, we need to explicitly
	// mark the request as failed to avoid having a dangling dependency.
	if ok && actualVtxID != expectedVtxID {
		t.Ctx.Log.Debug("incorrect vertex returned in Put",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("vtxID", actualVtxID),
			zap.Stringer("expectedVtxID", expectedVtxID),
		)
		// We assume that [vtx] is useless because it doesn't match what we
		// expected.
		return t.GetFailed(ctx, nodeID, requestID)
	}

	if t.Consensus.VertexIssued(vtx) || t.pending.Contains(actualVtxID) {
		t.metrics.numUselessPutBytes.Add(float64(len(vtxBytes)))
	}

	if _, err := t.issueFrom(ctx, nodeID, vtx); err != nil {
		return err
	}
	return t.attemptToIssueTxs(ctx)
}

func (t *Transitive) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// If the chain is linearized, we don't care that a get request failed, we
	// have already moved into snowman consensus.
	linearized, err := t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return err
	}
	if linearized {
		return nil
	}

	vtxID, ok := t.outstandingVtxReqs.Remove(nodeID, requestID)
	if !ok {
		t.Ctx.Log.Debug("unexpected GetFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	t.vtxBlocked.Abandon(ctx, vtxID)

	if t.outstandingVtxReqs.Len() == 0 {
		for txID := range t.missingTxs {
			t.txBlocked.Abandon(ctx, txID)
		}
		t.missingTxs.Clear()
	}

	// Track performance statistics
	t.metrics.numVtxRequests.Set(float64(t.outstandingVtxReqs.Len()))
	t.metrics.numMissingTxs.Set(float64(t.missingTxs.Len()))
	t.metrics.blockerVtxs.Set(float64(t.vtxBlocked.Len()))
	t.metrics.blockerTxs.Set(float64(t.txBlocked.Len()))
	return t.attemptToIssueTxs(ctx)
}

func (t *Transitive) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxID ids.ID) error {
	// If the chain is linearized, we don't care to attempt to issue any new
	// vertices.
	linearized, err := t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return err
	}
	if linearized {
		// Immediately respond to the query with the stop vertex.
		//
		// Invariant: This is done here, because the Consensus instance may have
		// never been initialized if bootstrapping accepted the stop vertex.
		edge := t.Manager.Edge(ctx)
		t.Sender.SendChits(ctx, nodeID, requestID, edge, edge)
		return nil
	}

	// Immediately respond to the query with the current consensus preferences.
	t.Sender.SendChits(ctx, nodeID, requestID, t.Consensus.Preferences().List(), t.Manager.Edge(ctx))

	// If we have [vtxID], attempt to put it into consensus, if we haven't
	// already. If we don't not have [vtxID], fetch it from [nodeID].
	if _, err := t.issueFromByID(ctx, nodeID, vtxID); err != nil {
		return err
	}

	return t.attemptToIssueTxs(ctx)
}

func (t *Transitive) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxBytes []byte) error {
	// If the chain is linearized, we don't care to attempt to issue any new
	// vertices.
	linearized, err := t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return err
	}
	if linearized {
		// Immediately respond to the query with the stop vertex.
		//
		// Invariant: This is done here, because the Consensus instance may have
		// never been initialized if bootstrapping accepted the stop vertex.
		edge := t.Manager.Edge(ctx)
		t.Sender.SendChits(ctx, nodeID, requestID, edge, edge)
		return nil
	}

	// Immediately respond to the query with the current consensus preferences.
	t.Sender.SendChits(ctx, nodeID, requestID, t.Consensus.Preferences().List(), t.Manager.Edge(ctx))

	vtx, err := t.Manager.ParseVtx(ctx, vtxBytes)
	if err != nil {
		t.Ctx.Log.Debug("failed to parse vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		t.Ctx.Log.Verbo("failed to parse vertex",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Binary("vertex", vtxBytes),
			zap.Error(err),
		)
		return nil
	}

	if t.Consensus.VertexIssued(vtx) || t.pending.Contains(vtx.ID()) {
		t.metrics.numUselessPushQueryBytes.Add(float64(len(vtxBytes)))
	}

	if _, err := t.issueFrom(ctx, nodeID, vtx); err != nil {
		return err
	}

	return t.attemptToIssueTxs(ctx)
}

func (t *Transitive) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, votes []ids.ID, accepted []ids.ID) error {
	// If the chain is linearized, we don't care to apply any votes.
	linearized, err := t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return err
	}
	if linearized {
		return nil
	}

	t.acceptedFrontiers.SetAcceptedFrontier(nodeID, accepted)

	v := &voter{
		t:         t,
		vdr:       nodeID,
		requestID: requestID,
		response:  votes,
	}
	for _, vote := range votes {
		if added, err := t.issueFromByID(ctx, nodeID, vote); err != nil {
			return err
		} else if !added {
			v.deps.Add(vote)
		}
	}

	t.vtxBlocked.Register(ctx, v)
	t.metrics.blockerVtxs.Set(float64(t.vtxBlocked.Len()))
	return t.attemptToIssueTxs(ctx)
}

func (t *Transitive) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// If the chain is linearized, we don't care to apply any votes.
	linearized, err := t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return err
	}
	if linearized {
		return nil
	}

	lastAccepted := t.acceptedFrontiers.AcceptedFrontier(nodeID)
	return t.Chits(ctx, nodeID, requestID, lastAccepted, lastAccepted)
}

func (*Transitive) Timeout(context.Context) error {
	return nil
}

func (t *Transitive) Gossip(ctx context.Context) error {
	edge := t.Manager.Edge(ctx)
	if len(edge) == 0 {
		t.Ctx.Log.Verbo("dropping gossip request as no vertices have been accepted")
		return nil
	}

	if err := t.uniformSampler.Initialize(uint64(len(edge))); err != nil {
		return err // Should never happen
	}
	indices, err := t.uniformSampler.Sample(1)
	if err != nil {
		return err // Also should never really happen because the edge has positive length
	}
	vtxID := edge[int(indices[0])]
	vtx, err := t.Manager.GetVtx(ctx, vtxID)
	if err != nil {
		t.Ctx.Log.Warn("dropping gossip request",
			zap.String("reason", "couldn't load vertex"),
			zap.Stringer("vtxID", vtxID),
			zap.Error(err),
		)
		return nil
	}

	t.Ctx.Log.Verbo("gossiping accepted vertex to the network",
		zap.Stringer("vtxID", vtxID),
	)
	t.Sender.SendGossip(ctx, vtx.Bytes())
	return nil
}

func (*Transitive) Halt(context.Context) {}

func (t *Transitive) Shutdown(ctx context.Context) error {
	t.Ctx.Log.Info("shutting down consensus engine")
	return t.VM.Shutdown(ctx)
}

func (t *Transitive) Notify(ctx context.Context, msg common.Message) error {
	// If the chain is linearized, we shouldn't be processing any messages from
	// the VM anymore.
	linearized, err := t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		return err
	}
	if linearized {
		return nil
	}

	switch msg {
	case common.PendingTxs:
		// After the linearization, we shouldn't be building any new vertices
		if cortinaTime, ok := version.CortinaTimes[t.Ctx.NetworkID]; ok && time.Now().After(cortinaTime) {
			return nil
		}

		txs := t.VM.PendingTxs(ctx)
		t.pendingTxs = append(t.pendingTxs, txs...)
		t.metrics.pendingTxs.Set(float64(len(t.pendingTxs)))
		return t.attemptToIssueTxs(ctx)

	case common.StopVertex:
		// stop vertex doesn't have any txs, issue directly!
		return t.issueStopVtx(ctx)

	default:
		t.Ctx.Log.Warn("received an unexpected message from the VM",
			zap.Stringer("messageString", msg),
		)
		return nil
	}
}

func (t *Transitive) Context() *snow.ConsensusContext {
	return t.Ctx
}

func (t *Transitive) Start(ctx context.Context, startReqID uint32) error {
	t.RequestID = startReqID
	// Load the vertices that were last saved as the accepted frontier
	edge := t.Manager.Edge(ctx)
	frontier := make([]avalanche.Vertex, 0, len(edge))
	for _, vtxID := range edge {
		if vtx, err := t.Manager.GetVtx(ctx, vtxID); err == nil {
			frontier = append(frontier, vtx)
		} else {
			t.Ctx.Log.Error("failed to load vertex from the frontier",
				zap.Stringer("vtxID", vtxID),
				zap.Error(err),
			)
		}
	}

	t.Ctx.Log.Info("consensus starting",
		zap.Int("lenFrontier", len(frontier)),
	)
	t.metrics.bootstrapFinished.Set(1)

	t.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.NormalOp,
	})
	if err := t.VM.SetState(ctx, snow.NormalOp); err != nil {
		return fmt.Errorf("failed to notify VM that consensus has started: %w",
			err)
	}
	return t.Consensus.Initialize(ctx, t.Ctx, t.Params, frontier)
}

func (t *Transitive) HealthCheck(ctx context.Context) (interface{}, error) {
	consensusIntf, consensusErr := t.Consensus.HealthCheck(ctx)
	vmIntf, vmErr := t.VM.HealthCheck(ctx)
	intf := map[string]interface{}{
		"consensus": consensusIntf,
		"vm":        vmIntf,
	}
	if consensusErr == nil {
		return intf, vmErr
	}
	if vmErr == nil {
		return intf, consensusErr
	}
	return intf, fmt.Errorf("vm: %w ; consensus: %v", vmErr, consensusErr)
}

func (t *Transitive) GetVM() common.VM {
	return t.VM
}

func (t *Transitive) GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
	// GetVtx returns a vertex by its ID.
	// Returns database.ErrNotFound if unknown.
	return t.Manager.GetVtx(ctx, vtxID)
}

func (t *Transitive) attemptToIssueTxs(ctx context.Context) error {
	err := t.errs.Err
	if err != nil {
		return err
	}

	t.pendingTxs, err = t.batch(ctx, t.pendingTxs, batchOption{limit: true})
	t.metrics.pendingTxs.Set(float64(len(t.pendingTxs)))
	return err
}

// If there are pending transactions from the VM, issue them.
// If we're not already at the limit for number of concurrent polls, issue a new
// query.
func (t *Transitive) repoll(ctx context.Context) {
	for i := t.polls.Len(); i < t.Params.ConcurrentRepolls && !t.errs.Errored(); i++ {
		t.issueRepoll(ctx)
	}
}

// issueFromByID issues the branch ending with vertex [vtxID] to consensus.
// Fetches [vtxID] if we don't have it locally.
// Returns true if [vtx] has been added to consensus (now or previously)
func (t *Transitive) issueFromByID(ctx context.Context, nodeID ids.NodeID, vtxID ids.ID) (bool, error) {
	vtx, err := t.Manager.GetVtx(ctx, vtxID)
	if err != nil {
		// We don't have [vtxID]. Request it.
		t.sendRequest(ctx, nodeID, vtxID)
		return false, nil
	}
	return t.issueFrom(ctx, nodeID, vtx)
}

// issueFrom issues the branch ending with [vtx] to consensus.
// Assumes we have [vtx] locally
// Returns true if [vtx] has been added to consensus (now or previously)
func (t *Transitive) issueFrom(ctx context.Context, nodeID ids.NodeID, vtx avalanche.Vertex) (bool, error) {
	issued := true
	// Before we issue [vtx] into consensus, we have to issue its ancestors.
	// Go through [vtx] and its ancestors. issue each ancestor that hasn't yet been issued.
	// If we find a missing ancestor, fetch it and note that we can't issue [vtx] yet.
	ancestry := vertex.NewHeap()
	ancestry.Push(vtx)
	for ancestry.Len() > 0 {
		vtx := ancestry.Pop()

		if t.Consensus.VertexIssued(vtx) {
			// This vertex has been issued --> its ancestors have been issued.
			// No need to try to issue it or its ancestors
			continue
		}
		if t.pending.Contains(vtx.ID()) {
			issued = false
			continue
		}

		parents, err := vtx.Parents()
		if err != nil {
			return false, err
		}
		// Ensure we have ancestors of this vertex
		for _, parent := range parents {
			if !parent.Status().Fetched() {
				// We don't have the parent. Request it.
				t.sendRequest(ctx, nodeID, parent.ID())
				// We're missing an ancestor so we can't have issued the vtx in this method's argument
				issued = false
			} else {
				// Come back to this vertex later to make sure it and its ancestors have been fetched/issued
				ancestry.Push(parent)
			}
		}

		// Queue up this vertex to be issued once its dependencies are met
		if err := t.issue(ctx, vtx); err != nil {
			return false, err
		}
	}
	return issued, nil
}

// issue queues [vtx] to be put into consensus after its dependencies are met.
// Assumes we have [vtx].
func (t *Transitive) issue(ctx context.Context, vtx avalanche.Vertex) error {
	vtxID := vtx.ID()

	// Add to set of vertices that have been queued up to be issued but haven't been yet
	t.pending.Add(vtxID)
	t.outstandingVtxReqs.RemoveAny(vtxID)

	// Will put [vtx] into consensus once dependencies are met
	i := &issuer{
		t:   t,
		vtx: vtx,
	}

	parents, err := vtx.Parents()
	if err != nil {
		return err
	}
	for _, parent := range parents {
		if !t.Consensus.VertexIssued(parent) {
			// This parent hasn't been issued yet. Add it as a dependency.
			i.vtxDeps.Add(parent.ID())
		}
	}

	txs, err := vtx.Txs(ctx)
	if err != nil {
		return err
	}
	txIDs := set.NewSet[ids.ID](len(txs))
	for _, tx := range txs {
		txIDs.Add(tx.ID())
	}

	for _, tx := range txs {
		deps, err := tx.Dependencies()
		if err != nil {
			return err
		}
		for _, dep := range deps {
			depID := dep.ID()
			if !txIDs.Contains(depID) && !t.Consensus.TxIssued(dep) {
				// This transaction hasn't been issued yet. Add it as a dependency.
				t.missingTxs.Add(depID)
				i.txDeps.Add(depID)
			}
		}
	}

	t.Ctx.Log.Verbo("vertex is blocking",
		zap.Stringer("vtxID", vtxID),
		zap.Int("numVtxDeps", i.vtxDeps.Len()),
		zap.Int("numTxDeps", i.txDeps.Len()),
	)

	// Wait until all the parents of [vtx] are added to consensus before adding [vtx]
	t.vtxBlocked.Register(ctx, &vtxIssuer{i: i})
	// Wait until all the parents of [tx] are added to consensus before adding [vtx]
	t.txBlocked.Register(ctx, &txIssuer{i: i})

	if t.outstandingVtxReqs.Len() == 0 {
		// There are no outstanding vertex requests but we don't have these transactions, so we're not getting them.
		for txID := range t.missingTxs {
			t.txBlocked.Abandon(ctx, txID)
		}
		t.missingTxs.Clear()
	}

	// Track performance statistics
	t.metrics.numVtxRequests.Set(float64(t.outstandingVtxReqs.Len()))
	t.metrics.numMissingTxs.Set(float64(t.missingTxs.Len()))
	t.metrics.numPendingVts.Set(float64(len(t.pending)))
	t.metrics.blockerVtxs.Set(float64(t.vtxBlocked.Len()))
	t.metrics.blockerTxs.Set(float64(t.txBlocked.Len()))
	return t.errs.Err
}

type batchOption struct {
	// if [force], allow for a conflict to be issued, and force each tx to be issued
	// otherwise, some txs may not be put into vertices that are issued.
	force bool
	// if [limit], stop when "Params.OptimalProcessing <= Consensus.NumProcessing"
	limit bool
}

// Batches [txs] into vertices and issue them.
func (t *Transitive) batch(ctx context.Context, txs []snowstorm.Tx, opt batchOption) ([]snowstorm.Tx, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	if opt.limit && t.Params.OptimalProcessing <= t.Consensus.NumProcessing() {
		return txs, nil
	}
	issuedTxs := set.Set[ids.ID]{}
	consumed := set.Set[ids.ID]{}
	orphans := t.Consensus.Orphans()
	start := 0
	end := 0
	for end < len(txs) {
		tx := txs[end]
		inputs := set.Set[ids.ID]{}
		inputs.Add(tx.InputIDs()...)
		overlaps := consumed.Overlaps(inputs)
		if end-start >= t.Params.BatchSize || (opt.force && overlaps) {
			if err := t.issueBatch(ctx, txs[start:end]); err != nil {
				return nil, err
			}
			if opt.limit && t.Params.OptimalProcessing <= t.Consensus.NumProcessing() {
				return txs[end:], nil
			}
			start = end
			consumed.Clear()
			overlaps = false
		}

		if txID := tx.ID(); !overlaps && // should never allow conflicting txs in the same vertex
			!issuedTxs.Contains(txID) && // shouldn't issue duplicated transactions to the same vertex
			(opt.force || t.Consensus.IsVirtuous(tx)) && // force allows for a conflict to be issued
			(!t.Consensus.TxIssued(tx) || orphans.Contains(txID)) { // should only reissue orphaned txs
			end++
			issuedTxs.Add(txID)
			consumed.Union(inputs)
		} else {
			newLen := len(txs) - 1
			txs[end] = txs[newLen]
			txs[newLen] = nil
			txs = txs[:newLen]
		}
	}

	if end > start {
		return txs[end:], t.issueBatch(ctx, txs[start:end])
	}
	return txs[end:], nil
}

// Issues a new poll for a preferred vertex in order to move consensus along
func (t *Transitive) issueRepoll(ctx context.Context) {
	preferredIDs := t.Consensus.Preferences()
	if preferredIDs.Len() == 0 {
		t.Ctx.Log.Error("re-query attempt was dropped due to no pending vertices")
		return
	}

	vtxID := preferredIDs.CappedList(1)[0]
	vdrIDs, err := t.Validators.Sample(t.Params.K) // Validators to sample
	if err != nil {
		t.Ctx.Log.Error("dropped re-query",
			zap.String("reason", "insufficient number of validators"),
			zap.Stringer("vtxID", vtxID),
			zap.Error(err),
		)
		return
	}

	vdrBag := bag.Bag[ids.NodeID]{} // IDs of validators to be sampled
	vdrBag.Add(vdrIDs...)

	vdrList := vdrBag.List()
	vdrSet := set.NewSet[ids.NodeID](len(vdrList))
	vdrSet.Add(vdrList...)

	// Poll the network
	t.RequestID++
	if t.polls.Add(t.RequestID, vdrBag) {
		t.Sender.SendPullQuery(ctx, vdrSet, t.RequestID, vtxID)
	}
}

// Puts a batch of transactions into a vertex and issues it into consensus.
func (t *Transitive) issueBatch(ctx context.Context, txs []snowstorm.Tx) error {
	t.Ctx.Log.Verbo("batching transactions into a new vertex",
		zap.Int("numTxs", len(txs)),
	)

	// Randomly select parents of this vertex from among the virtuous set
	virtuousIDs := t.Consensus.Virtuous().CappedList(t.Params.Parents)
	numVirtuousIDs := len(virtuousIDs)
	if err := t.uniformSampler.Initialize(uint64(numVirtuousIDs)); err != nil {
		return err
	}

	indices, err := t.uniformSampler.Sample(numVirtuousIDs)
	if err != nil {
		return err
	}

	parentIDs := make([]ids.ID, len(indices))
	for i, index := range indices {
		parentIDs[i] = virtuousIDs[int(index)]
	}

	vtx, err := t.Manager.BuildVtx(ctx, parentIDs, txs)
	if err != nil {
		t.Ctx.Log.Warn("error building new vertex",
			zap.Int("numParents", len(parentIDs)),
			zap.Int("numTxs", len(txs)),
			zap.Error(err),
		)
		return nil
	}

	return t.issue(ctx, vtx)
}

// to be triggered via X-Chain API
func (t *Transitive) issueStopVtx(ctx context.Context) error {
	// use virtuous frontier (accepted) as parents
	virtuousSet := t.Consensus.Virtuous()
	vtx, err := t.Manager.BuildStopVtx(ctx, virtuousSet.List())
	if err != nil {
		t.Ctx.Log.Warn("error building new stop vertex",
			zap.Int("numParents", virtuousSet.Len()),
			zap.Error(err),
		)
		return nil
	}
	return t.issue(ctx, vtx)
}

// Send a request to [vdr] asking them to send us vertex [vtxID]
func (t *Transitive) sendRequest(ctx context.Context, nodeID ids.NodeID, vtxID ids.ID) {
	if t.outstandingVtxReqs.Contains(vtxID) {
		t.Ctx.Log.Debug("not sending request for vertex",
			zap.String("reason", "existing outstanding request"),
			zap.Stringer("vtxID", vtxID),
		)
		return
	}
	t.RequestID++
	t.outstandingVtxReqs.Add(nodeID, t.RequestID, vtxID) // Mark that there is an outstanding request for this vertex
	t.Sender.SendGet(ctx, nodeID, t.RequestID, vtxID)
	t.metrics.numVtxRequests.Set(float64(t.outstandingVtxReqs.Len())) // Tracks performance statistics
}
