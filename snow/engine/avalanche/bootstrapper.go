// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"fmt"
	"math"
	"sync"

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
	chanSize  = 1000
	cacheSize = 2000
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

	numProcessed uint32 // TODO remove

	// outstandingRequests tracks which validators were asked for which containers in which requests
	outstandingRequests     common.Requests
	outstandingRequestsLock sync.Mutex

	// Incremented before an element is put into needed
	// Decremented at end of iteration of fetch()
	// --> If wg is 0, needed is empty and thread isn't in iteration of fetch()
	// Incremented before an element is put into toProcess
	// Decremented at end of iteration of process()
	// --> If wg is 0, toProcess is empty and thread isn't in iteration of process()
	// Incremented before an element is added to outstandingRequests
	// Decremented at end of function where an element is removed from outstandingRequests
	// --> If wg is 0, there are no outstanding requests
	// Invariant: If wg is 0, bootstrapping is done
	wg sync.WaitGroup

	// IDs of vertices that we need but don't have, and haven't sent a request for
	needed chan ids.ID

	// Vertices waiting to be processed
	toProcess chan avalanche.Vertex

	processedCache *cache.LRU

	// IDs of vertices that we have requested from other validators but haven't received
	pending    ids.Set
	finished   bool
	onFinished func() error
}

// Initialize this engine.
func (b *bootstrapper) Initialize(config BootstrapConfig) error {
	b.BootstrapConfig = config
	b.needed = make(chan ids.ID, chanSize)
	b.toProcess = make(chan avalanche.Vertex, chanSize)
	b.processedCache = &cache.LRU{Size: cacheSize}

	b.VtxBlocked.SetParser(&vtxParser{
		numAccepted: b.numBSVtx,
		numDropped:  b.numBSDroppedVtx,
		state:       b.State,
	})

	b.TxBlocked.SetParser(&txParser{
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

// Constantly fetch vertices we need but don't have
func (b *bootstrapper) fetch() {
	for {
		toFetch, chanOpen := <-b.needed // take a vertex we need
		if !chanOpen {                  // bootstrapping is done
			return
		}
		b.BootstrapConfig.Context.Log.Debug("in fetch. toFetch: %s", toFetch) // TODO remove

		// Make sure we don't already have this vertex
		if _, err := b.State.GetVertex(toFetch); err == nil {
			b.wg.Done() // Decremented at end of iteration of fetch()
			return
		}

		validators := b.BootstrapConfig.Validators.Sample(1) // validator to send request to
		if len(validators) == 0 {
			b.BootstrapConfig.Context.Log.Error("Dropping request for %s as there are no validators", toFetch)
			b.wg.Add(1)         // Incremented before an element is put into needed
			b.needed <- toFetch // TODO: What to do here? Right now this is an infinite loop...
			continue
		}
		validatorID := validators[0].ID()
		b.RequestID++

		b.wg.Add(1) // Incremented before an element is added to outstandingRequests
		b.outstandingRequestsLock.Lock()
		b.outstandingRequests.Add(validatorID, b.RequestID, toFetch)
		b.outstandingRequestsLock.Unlock()
		b.BootstrapConfig.Context.Log.Debug("in fetch. calling GetAncestor(%s, %d, %s)", validatorID, b.RequestID, toFetch) // TODO remove
		b.BootstrapConfig.Sender.GetAncestors(validatorID, b.RequestID, toFetch)                                            // request vertex and ancestors
		b.wg.Done()                                                                                                         // Decremented at end of iteration of fetch()
	}
}

// Constantly process vertices
func (b *bootstrapper) process() {
	for {
		vtx, chanOpen := <-b.toProcess // take a vertex we haven't processed
		if !chanOpen {
			return
		}

		// Check if we've already processed this vtx recently
		if _, processed := b.processedCache.Get(vtx.ID()); processed {
			b.wg.Done() // Decremented at end of iteration of process()
			continue
		} else {
			b.processedCache.Put(vtx.ID(), vtx.ID())
		}

		b.numProcessed++
		if b.numProcessed%1000 == 0 {
			b.BootstrapConfig.Context.Log.Info("processed %d vertices", b.numProcessed) // TODO remove
		}

		// process it
		b.BootstrapConfig.Context.Log.Debug("in process. vtx: %s", vtx.ID()) // TODO remove
		if err := b.VtxBlocked.Push(&vertexJob{
			numAccepted: b.numBSVtx,
			numDropped:  b.numBSDroppedVtx,
			vtx:         vtx,
		}); err == nil {
			b.numBSBlockedVtx.Inc()
		} else {
			b.BootstrapConfig.Context.Log.Fatal("couldn't push to vtxBlocked") // TODO make Verbo
		}

		for _, tx := range vtx.Txs() {
			if err := b.TxBlocked.Push(&txJob{
				numAccepted: b.numBSTx,
				numDropped:  b.numBSDroppedTx,
				tx:          tx,
			}); err == nil {
				b.numBSBlockedTx.Inc()
			} else {
				b.BootstrapConfig.Context.Log.Fatal("couldn't push to txBlocked") // TODO make Verbo
			}
		}

		for _, parent := range vtx.Parents() {
			b.BootstrapConfig.Context.Log.Debug("parent of %s is %s", vtx.ID(), parent.ID()) // TODO remove
			if parent.Status() == choices.Unknown {
				b.BootstrapConfig.Context.Log.Debug("parent %s is unknown. Adding to needed...", parent.ID()) // TODO remove
				b.wg.Add(1)                                                                                   // Incremented before an element is put into needed
				b.needed <- parent.ID()
			} else if parent.Status() == choices.Processing {
				b.BootstrapConfig.Context.Log.Debug("parent %s is processing. Adding to toProcess...", parent.ID()) // TODO remove
				b.wg.Add(1)                                                                                         // Incremented before an element is put into toProcess
				b.toProcess <- parent
			}
		}

		b.wg.Done() // Decremented at end of iteration of process()
	}
}

// Put ...
func (b *bootstrapper) Put(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) error {
	b.BootstrapConfig.Context.Log.Verbo("in Put(%s, %d, %s)", vdr, requestID, vtxID) // TODO remove
	vtx, err := b.State.ParseVertex(vtxBytes)                                        // Persists the vtx. vtx.Status() not Unknown.
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("Failed to parse vertex: %w", err)
		b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxBytes})
		return b.GetFailed(vdr, requestID)
	}
	parsedVtxID := vtx.ID() // Actual ID of the vertex we just got

	// The validator that sent this message said the ID of the vertex inside was [vtxID]
	// but actually it's [parsedVtxID]
	if !parsedVtxID.Equals(vtxID) {
		return b.GetFailed(vdr, requestID) // TODO is this right?
	}

	b.outstandingRequestsLock.Lock()
	expectedVtxID, ok := b.outstandingRequests.Remove(vdr, requestID)
	b.outstandingRequestsLock.Unlock()

	if !ok { // there was no outstanding request from this validator for a request with this ID
		if requestID != math.MaxUint32 { // request ID of math.MaxUint32 means the put was a gossip message. In that case, just return.
			b.BootstrapConfig.Context.Log.Debug("Unexpected Put. There is no outstanding request to %s with request ID %d", vdr, requestID)
		}
		// Don't call b.wg.Done() because nothing was removed from b.outstandingRequests
		return nil
	}

	if !expectedVtxID.Equals(parsedVtxID) {
		b.BootstrapConfig.Context.Log.Debug("Put(%s, %d) contains vertex %s but should contain vertex %s.", vdr, requestID, parsedVtxID, expectedVtxID)
		b.outstandingRequestsLock.Lock()
		b.outstandingRequests.Add(vdr, requestID, expectedVtxID) // Just going to be removed by GetFailed...TODO is there a better way to do this?
		b.outstandingRequestsLock.Unlock()
		// Don't call b.wg.Done() because nothing was removed from b.outstandingRequests
		return b.GetFailed(vdr, requestID)
	}

	switch vtx.Status() {
	case choices.Accepted, choices.Rejected:
		return nil
	case choices.Unknown:
		return fmt.Errorf("status of vtx %s is Unknown after it was parsed", vtxID)
	}

	b.wg.Add(1) // Incremented before an element is put into toProcess
	b.toProcess <- vtx
	b.wg.Done() // Decremented at end of function where an element is removed from outstandingRequests

	return nil
}

// PutAncestor ...
func (b *bootstrapper) PutAncestor(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) error {
	b.BootstrapConfig.Context.Log.Debug("in PutAncestor(%s, %d, %s)", vdr, requestID, vtxID) // TODO remove
	_, err := b.State.ParseVertex(vtxBytes)                                                  // Persists the vtx
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("Failed to parse vertex: %w", err)
		b.BootstrapConfig.Context.Log.Verbo("vertex: %s", formatting.DumpBytes{Bytes: vtxBytes})
	}
	return nil
}

// GetFailed is called when a Get message we sent fails
func (b *bootstrapper) GetFailed(vdr ids.ShortID, requestID uint32) error {
	b.BootstrapConfig.Context.Log.Debug("in GetFailed(%s, %d)", vdr, requestID) // TODO remove
	b.outstandingRequestsLock.Lock()
	vtxID, ok := b.outstandingRequests.Remove(vdr, requestID)
	b.outstandingRequestsLock.Unlock()
	if !ok {
		b.BootstrapConfig.Context.Log.Verbo("GetFailed(%s, %d) called but there was no outstanding request to this validator with this ID", vdr, requestID)
		return nil
	}
	// Send another request for this
	b.wg.Add(1) // Incremented before an element is put into needed
	b.needed <- vtxID
	b.wg.Done() // Decremented at end of function where an element is removed from outstandingRequests
	return nil
}

// ForceAccepted ...
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs ids.Set) error {
	b.BootstrapConfig.Context.Log.Debug("in forceAccepted") // TODO remove
	if acceptedContainerIDs.Len() == 0 {
		b.finish()
		return nil
	}

	for _, vtxID := range acceptedContainerIDs.List() {
		b.BootstrapConfig.Context.Log.Debug("in forceAccepted. vtxID: %s", vtxID) // TODO remove
		vtx, err := b.State.GetVertex(vtxID)
		if err != nil || vtx.Status() == choices.Unknown {
			b.BootstrapConfig.Context.Log.Debug("in forceAccepted. adding %s to needed", vtxID) // TODO remove
			b.wg.Add(1)                                                                         // Incremented before an element is put into needed
			b.needed <- vtxID
		} else if vtx.Status() == choices.Processing {
			b.BootstrapConfig.Context.Log.Debug("in forceAccepted. adding: %s to toProcess", vtx.ID()) // TODO remove
			b.wg.Add(1)                                                                                // Incremented before an element is put into toProcess
			b.toProcess <- vtx
		}
	}

	// TODO start threads
	go b.fetch()
	go b.process()
	go func() {
		b.wg.Wait() // wait until bootstrapping is done
		b.finish()
	}()
	return nil
}

// Finish bootstrapping
func (b *bootstrapper) finish() {
	if b.finished {
		return
	}
	b.BootstrapConfig.Context.Log.Info("bootstrapping finished fetching vertices. executing state transitions...")

	b.executeAll(b.TxBlocked, b.numBSBlockedTx)
	b.executeAll(b.VtxBlocked, b.numBSBlockedVtx)

	// Start consensus
	b.onFinished()
	close(b.toProcess)
	close(b.needed)
	b.finished = true
}

func (b *bootstrapper) executeAll(jobs *queue.Jobs, numBlocked prometheus.Gauge) {
	for job, err := jobs.Pop(); err == nil; job, err = jobs.Pop() {
		numBlocked.Dec()
		b.BootstrapConfig.Context.Log.Debug("Executing: %s", job.ID())
		if err := jobs.Execute(job); err != nil {
			b.BootstrapConfig.Context.Log.Warn("Error executing: %s", err)
		}
	}
}
