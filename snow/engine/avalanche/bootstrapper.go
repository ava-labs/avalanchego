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

	// number of vertices fetched so far
	numFetched uint32

	// tracks which validators were asked for which containers in which requests
	outstandingRequests common.Requests

	// IDs of vertices that we will send a GetAncestors request for once we are
	// not at the max number of outstanding requests
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

// CurrentAcceptedFrontier returns the set of vertices that this node has accepted
// that have no accepted children
func (b *bootstrapper) CurrentAcceptedFrontier() ids.Set {
	acceptedFrontier := ids.Set{}
	acceptedFrontier.Add(b.State.Edge()...)
	return acceptedFrontier
}

// FilterAccepted returns the IDs of vertices in [containerIDs] that this node has accepted
func (b *bootstrapper) FilterAccepted(containerIDs ids.Set) ids.Set {
	acceptedVtxIDs := ids.Set{}
	for _, vtxID := range containerIDs.List() {
		if vtx, err := b.State.GetVertex(vtxID); err == nil && vtx.Status() == choices.Accepted {
			acceptedVtxIDs.Add(vtxID)
		}
	}
	return acceptedVtxIDs
}

// Add the vertices in [vtxIDs] to the set of vertices that we need to fetch,
// and then fetch vertices (and their ancestors) until either there are no more
// to fetch or we are at the maximum number of outstanding requests.
func (b *bootstrapper) fetch(vtxIDs ...ids.ID) error {
	b.needToFetch.Add(vtxIDs...)
	for b.needToFetch.Len() > 0 && b.outstandingRequests.Len() < common.MaxOutstandingRequests {
		vtxID := b.needToFetch.CappedList(1)[0]
		b.needToFetch.Remove(vtxID)

		// Make sure we haven't already requested this vertex
		if b.outstandingRequests.Contains(vtxID) {
			continue
		}

		// Make sure we don't already have this vertex
		if _, err := b.State.GetVertex(vtxID); err == nil {
			continue
		}

		validators, err := b.BootstrapConfig.Validators.Sample(1) // validator to send request to
		if err != nil {
			return fmt.Errorf("Dropping request for %s as there are no validators", vtxID)
		}
		validatorID := validators[0].ID()
		b.RequestID++

		b.outstandingRequests.Add(validatorID, b.RequestID, vtxID)
		b.BootstrapConfig.Sender.GetAncestors(validatorID, b.RequestID, vtxID) // request vertex and ancestors
	}
	return b.finish()
}

// Process the vertices in [vtxs].
func (b *bootstrapper) process(vtxs ...avalanche.Vertex) error {
	// Vertices that we need to process. Store them in a heap for deduplication
	// and so we always process vertices further down in the DAG first. This helps
	// to reduce the number of repeated DAG traversals.
	toProcess := newMaxVertexHeap()
	for _, vtx := range vtxs {
		if _, ok := b.processedCache.Get(vtx.ID()); !ok { // only process a vertex if we haven't already
			toProcess.Push(vtx)
		}
	}

	for toProcess.Len() > 0 { // While there are unprocessed vertices
		vtx := toProcess.Pop() // Get an unknown vertex or one furthest down the DAG
		vtxID := vtx.ID()

		switch vtx.Status() {
		case choices.Unknown:
			b.needToFetch.Add(vtxID) // We don't have this vertex locally. Mark that we need to fetch it.
		case choices.Rejected:
			b.needToFetch.Remove(vtxID) // We have this vertex locally. Mark that we don't need to fetch it.
			return fmt.Errorf("tried to accept %s even though it was previously rejected", vtx.ID())
		case choices.Processing:
			b.needToFetch.Remove(vtxID)

			if err := b.VtxBlocked.Push(&vertexJob{ // Add to queue of vertices to execute when bootstrapping finishes.
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
			txs, err := vtx.Txs()
			if err != nil {
				return err
			}
			for _, tx := range txs { // Add transactions to queue of transactions to execute when bootstrapping finishes.
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
			parents, err := vtx.Parents()
			if err != nil {
				return err
			}
			for _, parent := range parents { // Process the parents of this vertex (traverse up the DAG)
				if _, ok := b.processedCache.Get(parent.ID()); !ok { // But only if we haven't processed the parent
					toProcess.Push(parent)
				}
			}
			height, err := vtx.Height()
			if err != nil {
				return err
			}
			if height%stripeDistance < stripeWidth { // See comment for stripeDistance
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

	return b.fetch()
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

	requestedVtxID, requested := b.outstandingRequests.Remove(vdr, requestID)
	vtx, err := b.State.ParseVertex(vtxs[0]) // first vertex should be the one we requested in GetAncestors request
	if err != nil {
		if !requested {
			b.BootstrapConfig.Context.Log.Debug("failed to parse unrequested vertex from %s with requestID %d: %s", vdr, requestID, err)
			return nil
		}

		b.BootstrapConfig.Context.Log.Debug("failed to parse requested vertex %s: %s", requestedVtxID, err)
		b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxs[0]})
		return b.fetch(requestedVtxID)
	}

	vtxID := vtx.ID()
	// If the vertex is neither the requested vertex nor a needed vertex, return early and re-fetch if necessary
	if requested && !requestedVtxID.Equals(vtxID) {
		b.BootstrapConfig.Context.Log.Debug("received incorrect vertex from %s with vertexID %s", vdr, vtxID)
		return b.fetch(requestedVtxID)
	}
	if !requested && !b.outstandingRequests.Contains(vtxID) && !b.needToFetch.Contains(vtxID) {
		b.BootstrapConfig.Context.Log.Debug("received un-needed vertex from %s with vertexID %s", vdr, vtxID)
		return nil
	}

	// Do not remove from outstanding requests if this did not answer a specific outstanding request
	// to ensure that real responses are not dropped in favor of potentially byzantine MultiPut messages that
	// could force the node to bootstrap 1 vertex at a time.
	b.needToFetch.Remove(vtxID)

	// All vertices added to [processVertices] have received transitive votes from the accepted frontier
	processVertices := make([]avalanche.Vertex, 1, len(vtxs)) // Process all of the valid vertices in this message
	processVertices[0] = vtx
	eligibleVertices := ids.Set{}
	parents, err := vtx.Parents()
	if err != nil {
		return err
	}
	for _, parent := range parents {
		eligibleVertices.Add(parent.ID())
	}

	for _, vtxBytes := range vtxs[1:] { // Parse/persist all the vertices
		vtx, err := b.State.ParseVertex(vtxBytes) // Persists the vtx
		if err != nil {
			b.BootstrapConfig.Context.Log.Debug("failed to parse vertex: %s", err)
			b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxBytes})
			break
		}
		vtxID := vtx.ID()
		if !eligibleVertices.Contains(vtxID) {
			b.BootstrapConfig.Context.Log.Debug("received vertex that should not have been included in MultiPut from %s with vertexID %s", vdr, vtxID)
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

// ForceAccepted starts bootstrapping. Process the vertices in [accepterContainerIDs].
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs ids.Set) error {
	if err := b.VM.Bootstrapping(); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	toProcess := make([]avalanche.Vertex, 0, acceptedContainerIDs.Len())
	for _, vtxID := range acceptedContainerIDs.List() {
		if vtx, err := b.State.GetVertex(vtxID); err == nil {
			toProcess = append(toProcess, vtx) // Process this vertex.
		} else {
			b.needToFetch.Add(vtxID) // We don't have this vertex. Mark that we have to fetch it.
		}
	}
	return b.process(toProcess...)
}

// Finish bootstrapping
func (b *bootstrapper) finish() error {
	// If there are outstanding requests for vertices or we still need to fetch vertices, we can't finish
	if b.finished || b.outstandingRequests.Len() > 0 || b.needToFetch.Len() > 0 {
		return nil
	}

	b.BootstrapConfig.Context.Log.Info("finished fetching %d vertices. executing transaction state transitions...",
		b.numFetched)
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
	b.BootstrapConfig.Context.Log.Info("executed %d operations", numExecuted)
	return nil
}
