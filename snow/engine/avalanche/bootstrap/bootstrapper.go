// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/version"
)

const (
	// We cache processed vertices where height = c * stripeDistance for c = {1,2,3...}
	// This forms a "stripe" of cached DAG vertices at height stripeDistance, 2*stripeDistance, etc.
	// This helps to limit the number of repeated DAG traversals performed
	stripeDistance = 2000
	stripeWidth    = 5
	cacheSize      = 100000

	// Parameters for delaying bootstrapping to avoid potential CPU burns
	bootstrappingDelay = 10 * time.Second
)

var (
	_ common.BootstrapableEngine = &bootstrapper{}

	errUnexpectedTimeout = errors.New("unexpected timeout fired")
)

func New(config Config, onFinished func(lastReqID uint32) error) (common.BootstrapableEngine, error) {
	b := &bootstrapper{
		Config: config,

		PutHandler:   common.NewNoOpPutHandler(config.Ctx.Log),
		QueryHandler: common.NewNoOpQueryHandler(config.Ctx.Log),
		ChitsHandler: common.NewNoOpChitsHandler(config.Ctx.Log),
		AppHandler:   common.NewNoOpAppHandler(config.Ctx.Log),

		processedCache:           &cache.LRU{Size: cacheSize},
		Fetcher:                  common.Fetcher{OnFinished: onFinished},
		executedStateTransitions: math.MaxInt32,
	}

	if err := b.metrics.Initialize("bs", config.Ctx.Registerer); err != nil {
		return nil, err
	}

	if err := b.VtxBlocked.SetParser(&vtxParser{
		log:         config.Ctx.Log,
		numAccepted: b.numAcceptedVts,
		numDropped:  b.numDroppedVts,
		manager:     b.Manager,
	}); err != nil {
		return nil, err
	}

	if err := b.TxBlocked.SetParser(&txParser{
		log:         config.Ctx.Log,
		numAccepted: b.numAcceptedTxs,
		numDropped:  b.numDroppedTxs,
		vm:          b.VM,
	}); err != nil {
		return nil, err
	}

	config.Config.Bootstrapable = b
	b.Bootstrapper = common.NewCommonBootstrapper(config.Config)
	return b, nil
}

type bootstrapper struct {
	Config

	// list of NoOpsHandler for messages dropped by bootstrapper
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler

	common.Bootstrapper
	common.Fetcher
	metrics

	started bool

	// IDs of vertices that we will send a GetAncestors request for once we are
	// not at the max number of outstanding requests
	needToFetch ids.Set

	// Contains IDs of vertices that have recently been processed
	processedCache *cache.LRU
	// number of state transitions executed
	executedStateTransitions int

	awaitingTimeout bool
}

// Clear implements common.Bootstrapable interface
func (b *bootstrapper) Clear() error {
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

// Ancestors handles the receipt of multiple containers. Should be received in response to a GetAncestors message to [vdr]
// with request ID [requestID]. Expects vtxs[0] to be the vertex requested in the corresponding GetAncestors.
func (b *bootstrapper) Ancestors(vdr ids.ShortID, requestID uint32, vtxs [][]byte) error {
	lenVtxs := len(vtxs)
	if lenVtxs == 0 {
		b.Ctx.Log.Debug("Ancestors(%s, %d) contains no vertices", vdr, requestID)
		return b.GetAncestorsFailed(vdr, requestID)
	}
	if lenVtxs > b.Config.AncestorsMaxContainersReceived {
		vtxs = vtxs[:b.Config.AncestorsMaxContainersReceived]
		b.Ctx.Log.Debug("ignoring %d containers in Ancestors(%s, %d)",
			lenVtxs-b.Config.AncestorsMaxContainersReceived, vdr, requestID)
	}

	requestedVtxID, requested := b.OutstandingRequests.Remove(vdr, requestID)
	vtx, err := b.Manager.ParseVtx(vtxs[0]) // first vertex should be the one we requested in GetAncestors request
	if err != nil {
		if !requested {
			b.Ctx.Log.Debug("failed to parse unrequested vertex from %s with requestID %d: %s", vdr, requestID, err)
			return nil
		}
		b.Ctx.Log.Debug("failed to parse requested vertex %s: %s", requestedVtxID, err)
		b.Ctx.Log.Verbo("vertex: %s", formatting.DumpBytes(vtxs[0]))
		return b.fetch(requestedVtxID)
	}

	vtxID := vtx.ID()
	// If the vertex is neither the requested vertex nor a needed vertex, return early and re-fetch if necessary
	if requested && requestedVtxID != vtxID {
		b.Ctx.Log.Debug("received incorrect vertex from %s with vertexID %s", vdr, vtxID)
		return b.fetch(requestedVtxID)
	}
	if !requested && !b.OutstandingRequests.Contains(vtxID) && !b.needToFetch.Contains(vtxID) {
		b.Ctx.Log.Debug("received un-needed vertex from %s with vertexID %s", vdr, vtxID)
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
	eligibleVertices := ids.NewSet(len(parents))
	for _, parent := range parents {
		eligibleVertices.Add(parent.ID())
	}

	for _, vtxBytes := range vtxs[1:] { // Parse/persist all the vertices
		vtx, err := b.Manager.ParseVtx(vtxBytes) // Persists the vtx
		if err != nil {
			b.Ctx.Log.Debug("failed to parse vertex: %s", err)
			b.Ctx.Log.Verbo("vertex: %s", formatting.DumpBytes(vtxBytes))
			break
		}
		vtxID := vtx.ID()
		if !eligibleVertices.Contains(vtxID) {
			b.Ctx.Log.Debug("received vertex that should not have been included in Ancestors from %s with vertexID %s", vdr, vtxID)
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

	return b.process(processVertices...)
}

// GetAncestorsFailed implements the AncestorsHandler interface
func (b *bootstrapper) GetAncestorsFailed(vdr ids.ShortID, requestID uint32) error {
	vtxID, ok := b.OutstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.Ctx.Log.Debug("GetAncestorsFailed(%s, %d) called but there was no outstanding request to this validator with this ID", vdr, requestID)
		return nil
	}
	// Send another request for the vertex
	return b.fetch(vtxID)
}

// Connected implements the InternalHandler interface.
func (b *bootstrapper) Connected(nodeID ids.ShortID, nodeVersion version.Application) error {
	if err := b.VM.Connected(nodeID, nodeVersion); err != nil {
		return err
	}

	if err := b.WeightTracker.AddWeightForNode(nodeID); err != nil {
		return err
	}

	if b.WeightTracker.EnoughConnectedWeight() && !b.started {
		b.started = true
		return b.Startup()
	}

	return nil
}

// Disconnected implements the InternalHandler interface.
func (b *bootstrapper) Disconnected(nodeID ids.ShortID) error {
	if err := b.VM.Disconnected(nodeID); err != nil {
		return err
	}

	return b.WeightTracker.RemoveWeightForNode(nodeID)
}

// Timeout implements the InternalHandler interface.
func (b *bootstrapper) Timeout() error {
	if !b.awaitingTimeout {
		return errUnexpectedTimeout
	}
	b.awaitingTimeout = false

	if !b.Config.Subnet.IsBootstrapped() {
		return b.Restart(true)
	}
	return b.finish()
}

// Gossip implements the InternalHandler interface.
func (b *bootstrapper) Gossip() error { return nil }

// Shutdown implements the InternalHandler interface.
func (b *bootstrapper) Shutdown() error { return nil }

// Notify implements the InternalHandler interface.
func (b *bootstrapper) Notify(common.Message) error { return nil }

// Context implements the common.Engine interface.
func (b *bootstrapper) Context() *snow.ConsensusContext { return b.Config.Ctx }

// Start implements the common.Engine interface.
func (b *bootstrapper) Start(startReqID uint32) error {
	b.Ctx.Log.Info("Starting bootstrap...")
	b.Ctx.SetState(snow.Bootstrapping)
	b.Config.SharedCfg.RequestID = startReqID

	if b.WeightTracker.EnoughConnectedWeight() {
		return nil
	}

	return b.Startup()
}

// HealthCheck implements the common.Engine interface.
func (b *bootstrapper) HealthCheck() (interface{}, error) {
	vmIntf, vmErr := b.VM.HealthCheck()
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

// GetVM implements the common.Engine interface.
func (b *bootstrapper) GetVM() common.VM { return b.VM }

// Add the vertices in [vtxIDs] to the set of vertices that we need to fetch,
// and then fetch vertices (and their ancestors) until either there are no more
// to fetch or we are at the maximum number of outstanding requests.
func (b *bootstrapper) fetch(vtxIDs ...ids.ID) error {
	b.needToFetch.Add(vtxIDs...)
	for b.needToFetch.Len() > 0 && b.OutstandingRequests.Len() < common.MaxOutstandingGetAncestorsRequests {
		vtxID := b.needToFetch.CappedList(1)[0]
		b.needToFetch.Remove(vtxID)

		// Make sure we haven't already requested this vertex
		if b.OutstandingRequests.Contains(vtxID) {
			continue
		}

		// Make sure we don't already have this vertex
		if _, err := b.Manager.GetVtx(vtxID); err == nil {
			continue
		}

		validators, err := b.Config.Beacons.Sample(1) // validator to send request to
		if err != nil {
			return fmt.Errorf("dropping request for %s as there are no validators", vtxID)
		}
		validatorID := validators[0].ID()
		b.Config.SharedCfg.RequestID++

		b.OutstandingRequests.Add(validatorID, b.Config.SharedCfg.RequestID, vtxID)
		b.Config.Sender.SendGetAncestors(validatorID, b.Config.SharedCfg.RequestID, vtxID) // request vertex and ancestors
	}
	return b.checkFinish()
}

// Process the vertices in [vtxs].
func (b *bootstrapper) process(vtxs ...avalanche.Vertex) error {
	// Vertices that we need to process. Store them in a heap for deduplication
	// and so we always process vertices further down in the DAG first. This helps
	// to reduce the number of repeated DAG traversals.
	toProcess := vertex.NewHeap()
	for _, vtx := range vtxs {
		vtxID := vtx.ID()
		if _, ok := b.processedCache.Get(vtxID); !ok { // only process a vertex if we haven't already
			toProcess.Push(vtx)
		} else {
			b.VtxBlocked.RemoveMissingID(vtxID)
		}
	}

	vtxHeightSet := ids.Set{}
	prevHeight := uint64(0)

	for toProcess.Len() > 0 { // While there are unprocessed vertices
		if b.Halted() {
			return nil
		}

		vtx := toProcess.Pop() // Get an unknown vertex or one furthest down the DAG
		vtxID := vtx.ID()

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
			if pushed, err := b.VtxBlocked.Push(&vertexJob{
				log:         b.Ctx.Log,
				numAccepted: b.numAcceptedVts,
				numDropped:  b.numDroppedVts,
				vtx:         vtx,
			}); err != nil {
				return err
			} else if !pushed {
				// If the vertex is already on the queue, then we have already
				// pushed [vtx]'s transactions and traversed into its parents.
				continue
			}

			txs, err := vtx.Txs()
			if err != nil {
				return err
			}
			for _, tx := range txs {
				// Add to queue of txs to execute when bootstrapping finishes.
				if pushed, err := b.TxBlocked.Push(&txJob{
					log:         b.Ctx.Log,
					numAccepted: b.numAcceptedTxs,
					numDropped:  b.numDroppedTxs,
					tx:          tx,
				}); err != nil {
					return err
				} else if pushed {
					b.numFetchedTxs.Inc()
				}
			}

			b.numFetchedVts.Inc()

			verticesFetchedSoFar := b.VtxBlocked.Jobs.PendingJobs()
			if verticesFetchedSoFar%common.StatusUpdateFrequency == 0 { // Periodically print progress
				if !b.Config.SharedCfg.Restarted {
					b.Ctx.Log.Info("fetched %d vertices", verticesFetchedSoFar)
				} else {
					b.Ctx.Log.Debug("fetched %d vertices", verticesFetchedSoFar)
				}
			}

			parents, err := vtx.Parents()
			if err != nil {
				return err
			}
			for _, parent := range parents { // Process the parents of this vertex (traverse up the DAG)
				parentID := parent.ID()
				if _, ok := b.processedCache.Get(parentID); !ok { // But only if we haven't processed the parent
					if !vtxHeightSet.Contains(parentID) {
						toProcess.Push(parent)
					}
				}
			}
			height, err := vtx.Height()
			if err != nil {
				return err
			}
			if height%stripeDistance < stripeWidth { // See comment for stripeDistance
				b.processedCache.Put(vtxID, nil)
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

	return b.fetch()
}

// ForceAccepted implements common.Bootstrapable interface
// ForceAccepted starts bootstrapping. Process the vertices in [accepterContainerIDs].
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs []ids.ID) error {
	if err := b.VM.SetState(snow.Bootstrapping); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	pendingContainerIDs := b.VtxBlocked.MissingIDs()
	// Append the list of accepted container IDs to pendingContainerIDs to ensure
	// we iterate over every container that must be traversed.
	pendingContainerIDs = append(pendingContainerIDs, acceptedContainerIDs...)
	b.Ctx.Log.Debug("Starting bootstrapping with %d missing vertices and %d from the accepted frontier", len(pendingContainerIDs), len(acceptedContainerIDs))
	toProcess := make([]avalanche.Vertex, 0, len(pendingContainerIDs))
	for _, vtxID := range pendingContainerIDs {
		if vtx, err := b.Manager.GetVtx(vtxID); err == nil {
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
	return b.process(toProcess...)
}

// checkFinish repeatedly executes pending transactions and requests new frontier blocks until there aren't any new ones
// after which it finishes the bootstrap process
func (b *bootstrapper) checkFinish() error {
	// If there are outstanding requests for vertices or we still need to fetch vertices, we can't finish
	pendingJobs := b.VtxBlocked.MissingIDs()
	if b.IsBootstrapped() || len(pendingJobs) > 0 || b.awaitingTimeout {
		return nil
	}

	if !b.Config.SharedCfg.Restarted {
		b.Ctx.Log.Info("bootstrapping fetched %d vertices. Executing transaction state transitions...", b.VtxBlocked.PendingJobs())
	} else {
		b.Ctx.Log.Debug("bootstrapping fetched %d vertices. Executing transaction state transitions...", b.VtxBlocked.PendingJobs())
	}

	_, err := b.TxBlocked.ExecuteAll(b.Config.Ctx, b, b.Config.SharedCfg.Restarted, b.Ctx.DecisionDispatcher)
	if err != nil || b.Halted() {
		return err
	}

	if !b.Config.SharedCfg.Restarted {
		b.Ctx.Log.Info("executing vertex state transitions...")
	} else {
		b.Ctx.Log.Debug("executing vertex state transitions...")
	}
	executedVts, err := b.VtxBlocked.ExecuteAll(b.Config.Ctx, b, b.Config.SharedCfg.Restarted, b.Ctx.ConsensusDispatcher)
	if err != nil || b.Halted() {
		return err
	}

	previouslyExecuted := b.executedStateTransitions
	b.executedStateTransitions = executedVts

	// Note that executedVts < c*previouslyExecuted is enforced so that the
	// bootstrapping process will terminate even as new vertices are being
	// issued.
	if executedVts > 0 && executedVts < previouslyExecuted/2 && b.Config.RetryBootstrap {
		b.Ctx.Log.Debug("checking for more vertices before finishing bootstrapping")
		return b.Restart(true)
	}

	// Notify the subnet that this chain is synced
	b.Config.Subnet.Bootstrapped(b.Ctx.ChainID)
	b.processedCache.Flush()

	// If the subnet hasn't finished bootstrapping, this chain should remain
	// syncing.
	if !b.Config.Subnet.IsBootstrapped() {
		if !b.Config.SharedCfg.Restarted {
			b.Ctx.Log.Info("waiting for the remaining chains in this subnet to finish syncing")
		} else {
			b.Ctx.Log.Debug("waiting for the remaining chains in this subnet to finish syncing")
		}
		// Restart bootstrapping after [bootstrappingDelay] to keep up to date
		// on the latest tip.
		b.Config.Timer.RegisterTimeout(bootstrappingDelay)
		b.awaitingTimeout = true
		return nil
	}

	return b.finish()
}

// Finish bootstrapping
func (b *bootstrapper) finish() error {
	if err := b.VM.SetState(snow.NormalOp); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has finished: %w",
			err)
	}

	// Start consensus
	return b.OnFinished(b.Config.SharedCfg.RequestID)
}
