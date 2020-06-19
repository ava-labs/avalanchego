// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"fmt"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// We cache processed vertices where height = c * stripeDistance for c = {1,2,3...}
	// This forms a "stripe" of cached DAG vertices at height stripeDistance, 2*stripeDistance, etc.
	// This helps to limit the number of repeated DAG traversals performed
	stripeDistance = 2000
	stripeWidth    = 5
	cacheSize      = 100000
)

// BootstrapConfig ...
type BootstrapConfig struct {
	common.Config

	// VtxBlocked tracks operations that are blocked on vertices
	// TxBlocked tracks operations that are blocked on transactions
	VtxBlocked, TxBlocked *queue.Jobs

	State State
	VM    DAGVM
}

type bootstrapper struct {
	BootstrapConfig
	metrics
	common.Bootstrapper

	// true if all of the vertices in the original accepted frontier have been processed
	processedStartingAcceptedFrontier bool

	// number of vertices fetched so far
	numFetched uint32

	// tracks which validators were asked for which containers in which requests
	outstandingRequests common.Requests

	// IDs of vertices that we will send a GetAncestors request for once we are not at the
	// max number of outstanding requests
	// Invariant: The intersection of needToFetch and outstandingRequests is empty
	needToFetch ids.Set

	// Contains IDs of vertices that have recently been processed
	processedCache *cache.LRU

	// true if bootstrapping is done
	finished bool

	// Called when bootstrapping is done
	onFinished func() error
}

// Initialize this engine.
func (b *bootstrapper) Initialize(config BootstrapConfig) error {
	b.BootstrapConfig = config
	b.processedCache = &cache.LRU{Size: cacheSize}

	b.VtxBlocked.SetParser(&vtxParser{
		log:         config.Context.Log,
		numAccepted: b.numBSVtx,
		numDropped:  b.numBSDroppedVtx,
		state:       b.State,
	})

	b.TxBlocked.SetParser(&txParser{
		log:         config.Context.Log,
		numAccepted: b.numBSTx,
		numDropped:  b.numBSDroppedTx,
		vm:          b.VM,
	})

	config.Bootstrapable = b
	b.Bootstrapper.Initialize(config.Config)
	return nil
}

// CurrentAcceptedFrontier ...
func (b *bootstrapper) CurrentAcceptedFrontier() ids.Set {
	acceptedFrontier := ids.Set{}
	acceptedFrontier.Add(b.State.Edge()...)
	return acceptedFrontier
}

// FilterAccepted ...
func (b *bootstrapper) FilterAccepted(containerIDs ids.Set) ids.Set {
	acceptedVtxIDs := ids.Set{}
	for _, vtxID := range containerIDs.List() {
		if vtx, err := b.State.GetVertex(vtxID); err == nil && vtx.Status() == choices.Accepted {
			acceptedVtxIDs.Add(vtxID)
		}
	}
	return acceptedVtxIDs
}

// Calls fetch for a pending vertex if there are any
func (b *bootstrapper) fetchANeededVtx() error {
	if b.needToFetch.Len() > 0 {
		return b.fetch(b.needToFetch.List()[0])
	}
	return nil
}

// Get vertex [vtxID] and its ancestors.
// If [vtxID] has already been requested or is already fetched, and there are
// unrequested vertices, requests one such vertex instead of [vtxID]
func (b *bootstrapper) fetch(vtxID ids.ID) error {
	// Make sure we haven't already requested this block
	if b.outstandingRequests.Contains(vtxID) {
		return b.fetchANeededVtx()
	}

	// Make sure we don't already have this vertex
	if _, err := b.State.GetVertex(vtxID); err == nil {
		if numPending := b.outstandingRequests.Len(); numPending == 0 && b.processedStartingAcceptedFrontier {
			return b.finish()
		}
		b.needToFetch.Remove(vtxID) // we have this vertex. no need to request it.
		return b.fetchANeededVtx()
	}

	// If we're already at maximum number of outstanding requests, queue for later
	if b.outstandingRequests.Len() >= common.MaxOutstandingRequests {
		b.needToFetch.Add(vtxID)
		return nil
	}

	validators := b.BootstrapConfig.Validators.Sample(1) // validator to send request to
	if len(validators) == 0 {
		return fmt.Errorf("Dropping request for %s as there are no validators", vtxID)
	}
	validatorID := validators[0].ID()
	b.RequestID++

	b.outstandingRequests.Add(validatorID, b.RequestID, vtxID)
	b.needToFetch.Remove(vtxID)                                            // maintains invariant that intersection with outstandingRequests is empty
	b.BootstrapConfig.Sender.GetAncestors(validatorID, b.RequestID, vtxID) // request vertex and ancestors
	return nil
}

// Process vertices
func (b *bootstrapper) process(vtxs ...avalanche.Vertex) error {
	toProcess := newMaxVertexHeap()
	for _, vtx := range vtxs {
		if _, ok := b.processedCache.Get(vtx.ID()); !ok { // only process if we haven't already
			toProcess.Push(vtx)
		}
	}

	for toProcess.Len() > 0 {
		vtx := toProcess.Pop()
		switch vtx.Status() {
		case choices.Unknown:
			if err := b.fetch(vtx.ID()); err != nil {
				return err
			}
		case choices.Rejected:
			return fmt.Errorf("tried to accept %s even though it was previously rejected", vtx.ID())
		case choices.Processing:
			if err := b.VtxBlocked.Push(&vertexJob{
				log:         b.BootstrapConfig.Context.Log,
				numAccepted: b.numBSVtx,
				numDropped:  b.numBSDroppedVtx,
				vtx:         vtx,
			}); err == nil {
				b.numBSBlockedVtx.Inc()
				b.numFetched++ // Progress tracker
				if b.numFetched%common.StatusUpdateFrequency == 0 {
					b.BootstrapConfig.Context.Log.Info("fetched %d vertices", b.numFetched)
				}
			} else {
				b.BootstrapConfig.Context.Log.Verbo("couldn't push to vtxBlocked: %s", err)
			}
			for _, tx := range vtx.Txs() {
				if err := b.TxBlocked.Push(&txJob{
					log:         b.BootstrapConfig.Context.Log,
					numAccepted: b.numBSTx,
					numDropped:  b.numBSDroppedTx,
					tx:          tx,
				}); err == nil {
					b.numBSBlockedTx.Inc()
				} else {
					b.BootstrapConfig.Context.Log.Verbo("couldn't push to txBlocked: %s", err)
				}
			}
			for _, parent := range vtx.Parents() {
				if _, ok := b.processedCache.Get(parent.ID()); !ok { // already processed this
					toProcess.Push(parent)
				}
			}
			if vtx.Height()%stripeDistance < stripeWidth {
				b.processedCache.Put(vtx.ID(), nil)
			}
		}
	}

	if err := b.VtxBlocked.Commit(); err != nil {
		return err
	}
	if err := b.TxBlocked.Commit(); err != nil {
		return err
	}

	if numPending := b.outstandingRequests.Len(); numPending == 0 && b.processedStartingAcceptedFrontier {
		return b.finish()
	}
	return nil
}

// MultiPut handles the receipt of multiple containers. Should be received in response to a GetAncestors message to [vdr]
// with request ID [requestID]. Expects vtxs[0] to be the vertex requested in the corresponding GetAncestors.
func (b *bootstrapper) MultiPut(vdr ids.ShortID, requestID uint32, vtxs [][]byte) error {
	if lenVtxs := len(vtxs); lenVtxs > common.MaxContainersPerMultiPut {
		b.BootstrapConfig.Context.Log.Debug("MultiPut(%s, %d) contains more than maximum number of vertices", vdr, requestID)
		return b.GetAncestorsFailed(vdr, requestID)
	} else if lenVtxs == 0 {
		b.BootstrapConfig.Context.Log.Debug("MultiPut(%s, %d) contains no vertices", vdr, requestID)
		return b.GetAncestorsFailed(vdr, requestID)
	}

	// Make sure this is in response to a request we made
	neededVtxID, needed := b.outstandingRequests.Remove(vdr, requestID)
	if !needed { // this message isn't in response to a request we made
		b.BootstrapConfig.Context.Log.Debug("received unexpected MultiPut from %s with ID %d", vdr, requestID)
		return nil
	}

	neededVtx, err := b.State.ParseVertex(vtxs[0]) // the vertex we requested
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("Failed to parse requested vertex %s: %w", neededVtxID, err)
		b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxs[0]})
		return b.fetch(neededVtxID)
	} else if actualID := neededVtx.ID(); !actualID.Equals(neededVtxID) {
		b.BootstrapConfig.Context.Log.Debug("expected the first block to be the requested block, %s, but is %s", neededVtxID, actualID)
		return b.fetch(neededVtxID)
	}

	processVertices := make([]avalanche.Vertex, 1, len(vtxs))
	processVertices[0] = neededVtx

	for _, vtxBytes := range vtxs[1:] { // Parse/persist all the vertices
		if vtx, err := b.State.ParseVertex(vtxBytes); err != nil { // Persists the vtx
			b.BootstrapConfig.Context.Log.Debug("Failed to parse vertex: %w", err)
			b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxBytes})
		} else {
			processVertices = append(processVertices, vtx)
			b.needToFetch.Remove(vtx.ID()) // No need to fetch this vertex since we have it now
		}
	}

	// Now there is one less outstanding request; send another if needed
	if err := b.fetchANeededVtx(); err != nil {
		return err
	}

	return b.process(processVertices...)
}

// GetAncestorsFailed is called when a GetAncestors message we sent fails
func (b *bootstrapper) GetAncestorsFailed(vdr ids.ShortID, requestID uint32) error {
	vtxID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.BootstrapConfig.Context.Log.Debug("GetAncestorsFailed(%s, %d) called but there was no outstanding request to this validator with this ID", vdr, requestID)
		return nil
	}
	// Send another request for the vertex
	return b.fetch(vtxID)
}

// ForceAccepted ...
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs ids.Set) error {
	if err := b.VM.Bootstrapping(); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	storedVtxs := make([]avalanche.Vertex, 0, acceptedContainerIDs.Len())
	for _, vtxID := range acceptedContainerIDs.List() {
		if vtx, err := b.State.GetVertex(vtxID); err == nil {
			storedVtxs = append(storedVtxs, vtx)
		} else if err := b.fetch(vtxID); err != nil {
			return err
		}
	}
	if err := b.process(storedVtxs...); err != nil {
		return err
	}
	b.processedStartingAcceptedFrontier = true

	if numPending := b.outstandingRequests.Len(); numPending == 0 {
		return b.finish()
	}
	return nil
}

// Finish bootstrapping
func (b *bootstrapper) finish() error {
	if b.finished {
		return nil
	}
	b.BootstrapConfig.Context.Log.Info("finished fetching vertices. executing transaction state transitions...")

	if err := b.executeAll(b.TxBlocked, b.numBSBlockedTx); err != nil {
		return err
	}

	b.BootstrapConfig.Context.Log.Info("executing vertex state transitions...")

	if err := b.executeAll(b.VtxBlocked, b.numBSBlockedVtx); err != nil {
		return err
	}

	if err := b.VM.Bootstrapped(); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has finished: %w",
			err)
	}

	// Start consensus
	if err := b.onFinished(); err != nil {
		return err
	}
	b.finished = true
	return nil
}

func (b *bootstrapper) executeAll(jobs *queue.Jobs, numBlocked prometheus.Gauge) error {
	numExecuted := 0
	for job, err := jobs.Pop(); err == nil; job, err = jobs.Pop() {
		numBlocked.Dec()
		b.BootstrapConfig.Context.Log.Debug("Executing: %s", job.ID())
		if err := jobs.Execute(job); err != nil {
			b.BootstrapConfig.Context.Log.Error("Error executing: %s", err)
			return err
		}
		if err := jobs.Commit(); err != nil {
			return err
		}
		numExecuted++
		if numExecuted%common.StatusUpdateFrequency == 0 { // Periodically print progress
			b.BootstrapConfig.Context.Log.Info("executed %d operations", numExecuted)
		}
	}
	return nil
}
