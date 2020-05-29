// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/prometheus/client_golang/prometheus"
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

	// IDs of vertices that we're already in the process of getting
	// TODO: Find a better way to track; this keeps every single vertex's ID in memory when bootstrapping from nothing
	seen ids.Set

	numFetched uint64 // number of vertices that have been fetched from validators

	// vtxReqs prevents asking validators for the same vertex
	vtxReqs common.Requests

	// IDs of vertices that we have requested from other validators but haven't received
	pending    ids.Set
	finished   bool
	onFinished func() error
}

// Initialize this engine.
func (b *bootstrapper) Initialize(config BootstrapConfig) error {
	b.BootstrapConfig = config

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

// ForceAccepted ...
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs ids.Set) error {
	for _, vtxID := range acceptedContainerIDs.List() {
		if err := b.fetch(vtxID); err != nil {
			return err
		}
	}

	if numPending := b.pending.Len(); numPending == 0 {
		// TODO: This typically indicates bootstrapping has failed, so this
		// should be handled appropriately
		return b.finish()
	}
	return nil
}

// Put ...
func (b *bootstrapper) Put(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) error {
	vtx, err := b.State.ParseVertex(vtxBytes)
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("ParseVertex failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: vtxBytes})

		return b.GetFailed(vdr, requestID)
	}

	if !b.pending.Contains(vtx.ID()) {
		b.BootstrapConfig.Context.Log.Debug("Validator %s sent an unrequested vertex:\n%s",
			vdr,
			formatting.DumpBytes{Bytes: vtxBytes})

		return b.GetFailed(vdr, requestID)
	}

	return b.addVertex(vtx)
}

// GetFailed ...
func (b *bootstrapper) GetFailed(vdr ids.ShortID, requestID uint32) error {
	vtxID, ok := b.vtxReqs.Remove(vdr, requestID)
	if !ok {
		b.BootstrapConfig.Context.Log.Debug("GetFailed called without sending the corresponding Get message from %s",
			vdr)
		return nil
	}

	b.sendRequest(vtxID)
	return nil
}

func (b *bootstrapper) fetch(vtxID ids.ID) error {
	if b.pending.Contains(vtxID) {
		return nil
	}

	vtx, err := b.State.GetVertex(vtxID)
	if err != nil {
		b.sendRequest(vtxID)
		return nil
	}
	return b.storeVertex(vtx)
}

func (b *bootstrapper) sendRequest(vtxID ids.ID) {
	validators := b.BootstrapConfig.Validators.Sample(1)
	if len(validators) == 0 {
		b.BootstrapConfig.Context.Log.Error("Dropping request for %s as there are no validators", vtxID)
		return
	}
	validatorID := validators[0].ID()
	b.RequestID++

	b.vtxReqs.RemoveAny(vtxID)
	b.vtxReqs.Add(validatorID, b.RequestID, vtxID)

	b.pending.Add(vtxID)
	b.BootstrapConfig.Sender.Get(validatorID, b.RequestID, vtxID)

	b.numBSPendingRequests.Set(float64(b.pending.Len()))
}

func (b *bootstrapper) addVertex(vtx avalanche.Vertex) error {
	if err := b.storeVertex(vtx); err != nil {
		return err
	}

	if numPending := b.pending.Len(); numPending == 0 {
		return b.finish()
	}
	return nil
}

func (b *bootstrapper) storeVertex(vtx avalanche.Vertex) error {
	vts := []avalanche.Vertex{vtx}
	b.numFetched++
	if b.numFetched%2500 == 0 { // perioidcally inform user of progress
		b.BootstrapConfig.Context.Log.Info("bootstrapping has fetched %d vertices", b.numFetched)
	}

	for len(vts) > 0 {
		newLen := len(vts) - 1
		vtx := vts[newLen]
		vts = vts[:newLen]

		vtxID := vtx.ID()
		switch status := vtx.Status(); status {
		case choices.Unknown:
			b.sendRequest(vtxID)
		case choices.Processing:
			b.pending.Remove(vtxID)

			if err := b.VtxBlocked.Push(&vertexJob{
				log:         b.BootstrapConfig.Context.Log,
				numAccepted: b.numBSVtx,
				numDropped:  b.numBSDroppedVtx,
				vtx:         vtx,
			}); err == nil {
				b.numBSBlockedVtx.Inc()
			} else {
				b.BootstrapConfig.Context.Log.Verbo("couldn't push to vtxBlocked")
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
					b.BootstrapConfig.Context.Log.Verbo("couldn't push to txBlocked")
				}
			}
			if err := b.TxBlocked.Commit(); err != nil {
				return err
			}
			for _, parent := range vtx.Parents() {
				if parentID := parent.ID(); !b.seen.Contains(parentID) {
					b.seen.Add(parentID)
					vts = append(vts, parent)
				}
			}
		case choices.Accepted:
			b.BootstrapConfig.Context.Log.Verbo("bootstrapping confirmed %s", vtxID)
		case choices.Rejected:
			return fmt.Errorf("bootstrapping wants to accept %s, however it was previously rejected", vtxID)
		}
	}

	numPending := b.pending.Len()
	b.numBSPendingRequests.Set(float64(numPending))
	return nil
}

func (b *bootstrapper) finish() error {
	if b.finished {
		return nil
	}
	b.BootstrapConfig.Context.Log.Info("bootstrapping finished fetching vertices. executing state transitions...")

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
	b.seen = ids.Set{}
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
