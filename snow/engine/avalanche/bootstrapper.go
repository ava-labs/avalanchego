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
	cacheSize = 3000
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

	// number of vertices processed so far
	numProcessed uint32

	// tracks which validators were asked for which containers in which requests
	outstandingRequests common.Requests

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

// Get vertex [vtxID] and its ancestors
func (b *bootstrapper) fetch(vtxID ids.ID) error {
	// Make sure we haven't already requested this block
	if b.outstandingRequests.Contains(vtxID) {
		return nil
	}

	// Make sure we don't already have this vertex
	if _, err := b.State.GetVertex(vtxID); err == nil {
		return nil
	}

	validators := b.BootstrapConfig.Validators.Sample(1) // validator to send request to
	if len(validators) == 0 {
		return fmt.Errorf("Dropping request for %s as there are no validators", vtxID)
	}
	validatorID := validators[0].ID()
	b.RequestID++

	b.outstandingRequests.Add(validatorID, b.RequestID, vtxID)
	b.BootstrapConfig.Sender.GetAncestors(validatorID, b.RequestID, vtxID) // request vertex and ancestors
	return nil
}

// Process vertices
func (b *bootstrapper) process(vtx avalanche.Vertex) error {
	toProcess := []avalanche.Vertex{vtx}
	for len(toProcess) > 0 {
		newLen := len(toProcess) - 1
		vtx := toProcess[newLen]
		toProcess = toProcess[:newLen]
		if _, ok := b.processedCache.Get(vtx.ID()); ok { // already processed this
			continue
		}
		b.numProcessed++ // Progress tracker
		if b.numProcessed%common.StatusUpdateFrequency == 0 {
			b.BootstrapConfig.Context.Log.Info("processed %d vertices", b.numProcessed)
		}

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
			} else {
				b.BootstrapConfig.Context.Log.Verbo("couldn't push to vtxBlocked: %s", err)
			}
			if err := b.VtxBlocked.Commit(); err != nil {
				return err
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
			if err := b.TxBlocked.Commit(); err != nil {
				return err
			}
			for _, parent := range vtx.Parents() {
				toProcess = append(toProcess, parent)
			}
			b.processedCache.Put(vtx.ID(), nil)
		}
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

	for _, vtxBytes := range vtxs { // Parse/persist all the vertices
		if _, err := b.State.ParseVertex(vtxBytes); err != nil { // Persists the vtx
			b.BootstrapConfig.Context.Log.Debug("Failed to parse vertex: %w", err)
			b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxBytes})
		}
	}

	return b.process(neededVtx)
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

	for _, vtxID := range acceptedContainerIDs.List() {
		if vtx, err := b.State.GetVertex(vtxID); err == nil {
			if err := b.process(vtx); err != nil {
				return err
			}
		} else if err := b.fetch(vtxID); err != nil {
			return err
		}
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
	b.BootstrapConfig.Context.Log.Info("finished fetching vertices. executing state transitions...")

	if err := b.executeAll(b.TxBlocked, b.numBSBlockedTx); err != nil {
		return err
	}
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
	}
	return nil
}
