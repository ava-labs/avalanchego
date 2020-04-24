// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
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

	pending    ids.Set
	finished   bool
	onFinished func()
}

// Initialize this engine.
func (b *bootstrapper) Initialize(config BootstrapConfig) {
	b.BootstrapConfig = config

	b.VtxBlocked.SetParser(&vtxParser{
		numAccepted: b.numBootstrappedVtx,
		numDropped:  b.numDroppedVtx,
		state:       b.State,
	})

	b.TxBlocked.SetParser(&txParser{
		numAccepted: b.numBootstrappedTx,
		numDropped:  b.numDroppedTx,
		vm:          b.VM,
	})

	config.Bootstrapable = b
	b.Bootstrapper.Initialize(config.Config)
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
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs ids.Set) {
	for _, vtxID := range acceptedContainerIDs.List() {
		b.fetch(vtxID)
	}

	if numPending := b.pending.Len(); numPending == 0 {
		// TODO: This typically indicates bootstrapping has failed, so this
		// should be handled appropriately
		b.finish()
	}
}

// Put ...
func (b *bootstrapper) Put(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) {
	b.BootstrapConfig.Context.Log.Verbo("Put called for vertexID %s", vtxID)

	if !b.pending.Contains(vtxID) {
		return
	}

	vtx, err := b.State.ParseVertex(vtxBytes)
	if err != nil {
		b.BootstrapConfig.Context.Log.Warn("ParseVertex failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: vtxBytes})
		b.GetFailed(vdr, requestID, vtxID)
		return
	}

	b.addVertex(vtx)
}

// GetFailed ...
func (b *bootstrapper) GetFailed(_ ids.ShortID, _ uint32, vtxID ids.ID) { b.sendRequest(vtxID) }

func (b *bootstrapper) fetch(vtxID ids.ID) {
	if b.pending.Contains(vtxID) {
		return
	}

	vtx, err := b.State.GetVertex(vtxID)
	if err != nil {
		b.sendRequest(vtxID)
		return
	}
	b.storeVertex(vtx)
}

func (b *bootstrapper) sendRequest(vtxID ids.ID) {
	validators := b.BootstrapConfig.Validators.Sample(1)
	if len(validators) == 0 {
		b.BootstrapConfig.Context.Log.Error("Dropping request for %s as there are no validators", vtxID)
		return
	}
	validatorID := validators[0].ID()
	b.RequestID++

	b.pending.Add(vtxID)
	b.BootstrapConfig.Sender.Get(validatorID, b.RequestID, vtxID)

	b.numPendingRequests.Set(float64(b.pending.Len()))
}

func (b *bootstrapper) addVertex(vtx avalanche.Vertex) {
	b.storeVertex(vtx)

	if numPending := b.pending.Len(); numPending == 0 {
		b.finish()
	}
}

func (b *bootstrapper) storeVertex(vtx avalanche.Vertex) {
	vts := []avalanche.Vertex{vtx}

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
				numAccepted: b.numBootstrappedVtx,
				numDropped:  b.numDroppedVtx,
				vtx:         vtx,
			}); err == nil {
				b.numBlockedVtx.Inc()
			}
			for _, tx := range vtx.Txs() {
				if err := b.TxBlocked.Push(&txJob{
					numAccepted: b.numBootstrappedVtx,
					numDropped:  b.numDroppedVtx,
					tx:          tx,
				}); err == nil {
					b.numBlockedTx.Inc()
				}
			}

			vts = append(vts, vtx.Parents()...)
		case choices.Accepted:
			b.BootstrapConfig.Context.Log.Verbo("Bootstrapping confirmed %s", vtxID)
		case choices.Rejected:
			b.BootstrapConfig.Context.Log.Error("Bootstrapping wants to accept %s, however it was previously rejected", vtxID)
		}
	}

	numPending := b.pending.Len()
	b.numPendingRequests.Set(float64(numPending))
}

func (b *bootstrapper) finish() {
	if b.finished {
		return
	}

	b.executeAll(b.TxBlocked, b.numBlockedTx)
	b.executeAll(b.VtxBlocked, b.numBlockedVtx)

	// Start consensus
	b.onFinished()
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
