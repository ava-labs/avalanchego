// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"fmt"
	"math"

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

	// number of vertices processed so far
	numProcessed uint32

	// outstandingRequests tracks which validators were asked for which containers in which requests
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

// Get a vertex and its ancestors
func (b *bootstrapper) fetch(vtxID ids.ID) error {
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
			b.processedCache.Put(vtx.ID(), vtx.ID())
		}
	}
	if numPending := b.outstandingRequests.Len(); numPending == 0 {
		return b.finish()
	}
	return nil
}

// Put handles the receipt of a vertex and processes it
func (b *bootstrapper) Put(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) error {
	vtx, err := b.State.ParseVertex(vtxBytes) // Persists the vtx. vtx.Status() not Unknown.
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("Failed to parse vertex: %w", err)
		b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxBytes})
		return b.GetFailed(vdr, requestID)
	}
	parsedVtxID := vtx.ID() // Actual ID of the vertex we just got

	// The validator that sent this message said the ID of the vertex inside was [vtxID]
	// but actually it's [parsedVtxID]
	if !parsedVtxID.Equals(vtxID) {
		b.BootstrapConfig.Context.Log.Debug("expected Put from %s to contain %s but contains %s. Request ID: %d", vdr, vtxID, parsedVtxID, requestID)
		return b.GetFailed(vdr, requestID)
	}

	expectedVtxID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok { // there was no outstanding request from this validator for a request with this ID
		if requestID != math.MaxUint32 { // request ID of math.MaxUint32 means the put was a gossip message. In that case, just return.
			b.BootstrapConfig.Context.Log.Debug("Unexpected Put. There is no outstanding request to %s with request ID %d", vdr, requestID)
		}
		return nil
	}

	if !expectedVtxID.Equals(parsedVtxID) {
		b.BootstrapConfig.Context.Log.Debug("Put(%s, %d) contains vertex %s but should contain vertex %s.", vdr, requestID, parsedVtxID, expectedVtxID)
		b.outstandingRequests.Add(vdr, requestID, expectedVtxID) // Just going to be removed by GetFailed
		return b.GetFailed(vdr, requestID)
	}

	return b.process(vtx) // Process this vtx
}

// MultiPut handles the receipt of multiple containers. Should be received in response to a GetAncestors message to [vdr]
// with request ID [requestID]
func (b *bootstrapper) MultiPut(vdr ids.ShortID, requestID uint32, vtxs [][]byte) error {
	b.BootstrapConfig.Context.Log.Verbo("in MultiPut(%s, %d). len(vtxs): %d", vdr, requestID, len(vtxs)) // TODO remove
	// Make sure this is in response to a request we made
	neededVtxID, needed := b.outstandingRequests.Remove(vdr, requestID)
	if !needed { // this message isn't in response to a request for a vertex we need
		if _, requested := b.outstandingRequests.Remove(vdr, requestID); !requested { // this message isn't in response to a request for a vertex we greedily requested
			b.BootstrapConfig.Context.Log.Debug("received unexpected MultiPut from %s with ID %d", vdr, requestID)
			return nil
		}
	}

	var neededVtx avalanche.Vertex = nil // the vertex that this MultiPut is in response to
	for i, vtxBytes := range vtxs {
		if i > common.MaxContainersPerMultiPut {
			b.BootstrapConfig.Context.Log.Debug("MultiPut from %s contains more than maximum number of vertices. Request ID: %d", vdr, requestID)
			break
		}
		vtx, err := b.State.ParseVertex(vtxBytes) // Persists the vtx
		if err != nil {
			b.BootstrapConfig.Context.Log.Debug("Failed to parse vertex: %w", err)
			b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxBytes})
		}
		if vtx.ID().Equals(neededVtxID) {
			neededVtx = vtx // found the vtx we wanted
		}
	}

	if !needed {
		return nil
	}

	// This MultiPut was supposed to include [neededVtxID] but it didn't
	if neededVtx == nil {
		b.outstandingRequests.Add(vdr, requestID, neededVtxID) // immediately removed by getFailed
		return b.GetFailed(vdr, requestID)
	}

	return b.process(neededVtx)
}

// GetFailed is called when a Get message we sent fails
func (b *bootstrapper) GetFailed(vdr ids.ShortID, requestID uint32) error {
	vtxID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.BootstrapConfig.Context.Log.Debug("GetFailed(%s, %d) called but there was no outstanding request to this validator with this ID", vdr, requestID)
		return nil
	}
	// Send another request for this
	return b.fetch(vtxID)
}

// ForceAccepted ...
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs ids.Set) error {
	for _, vtxID := range acceptedContainerIDs.List() {
		if vtx, err := b.State.GetVertex(vtxID); err == nil {
			b.process(vtx)
		} else if err := b.fetch(vtxID); err != nil {
			return err
		}
	}

	if numPending := b.outstandingRequests.Len(); numPending == 0 {
		// TODO: This typically indicates bootstrapping has failed, so this
		// should be handled appropriately
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
