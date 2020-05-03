// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/events"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/random"
)

// Transitive implements the Engine interface by attempting to fetch all
// transitive dependencies.
type Transitive struct {
	Config
	bootstrapper

	polls polls // track people I have asked for their preference

	// vtxReqs prevents asking validators for the same vertex
	vtxReqs common.Requests

	// missingTxs tracks transaction that are missing
	missingTxs, pending ids.Set

	// vtxBlocked tracks operations that are blocked on vertices
	// txBlocked tracks operations that are blocked on transactions
	vtxBlocked, txBlocked events.Blocker

	bootstrapped bool
}

// Initialize implements the Engine interface
func (t *Transitive) Initialize(config Config) {
	config.Context.Log.Info("Initializing Avalanche consensus")

	t.Config = config
	t.metrics.Initialize(config.Context.Log, config.Params.Namespace, config.Params.Metrics)

	t.onFinished = t.finishBootstrapping
	t.bootstrapper.Initialize(config.BootstrapConfig)

	t.polls.log = config.Context.Log
	t.polls.numPolls = t.numPolls
	t.polls.m = make(map[uint32]poll)
}

func (t *Transitive) finishBootstrapping() {
	// Load the vertices that were last saved as the accepted frontier
	frontier := []avalanche.Vertex(nil)
	for _, vtxID := range t.Config.State.Edge() {
		if vtx, err := t.Config.State.GetVertex(vtxID); err == nil {
			frontier = append(frontier, vtx)
		} else {
			t.Config.Context.Log.Error("Vertex %s failed to be loaded from the frontier with %s", vtxID, err)
		}
	}
	t.Consensus.Initialize(t.Config.Context, t.Params, frontier)
	t.bootstrapped = true
}

// Shutdown implements the Engine interface
func (t *Transitive) Shutdown() {
	t.Config.Context.Log.Info("Shutting down Avalanche consensus")
	t.Config.VM.Shutdown()
}

// Context implements the Engine interface
func (t *Transitive) Context() *snow.Context { return t.Config.Context }

// Get implements the Engine interface
func (t *Transitive) Get(vdr ids.ShortID, requestID uint32, vtxID ids.ID) {
	// If this engine has access to the requested vertex, provide it
	if vtx, err := t.Config.State.GetVertex(vtxID); err == nil {
		t.Config.Sender.Put(vdr, requestID, vtxID, vtx.Bytes())
	}
}

// Put implements the Engine interface
func (t *Transitive) Put(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) {
	t.Config.Context.Log.Verbo("Put called for vertexID %s", vtxID)

	if !t.bootstrapped {
		t.bootstrapper.Put(vdr, requestID, vtxID, vtxBytes)
		return
	}

	vtx, err := t.Config.State.ParseVertex(vtxBytes)
	if err != nil {
		t.Config.Context.Log.Warn("ParseVertex failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: vtxBytes})
		t.GetFailed(vdr, requestID)
		return
	}
	t.insertFrom(vdr, vtx)
}

// GetFailed implements the Engine interface
func (t *Transitive) GetFailed(vdr ids.ShortID, requestID uint32) {
	if !t.bootstrapped {
		t.bootstrapper.GetFailed(vdr, requestID)
		return
	}

	vtxID, ok := t.vtxReqs.Remove(vdr, requestID)
	if !ok {
		t.Config.Context.Log.Warn("GetFailed called without sending the corresponding Get message from %s",
			vdr)
		return
	}

	t.vtxBlocked.Abandon(vtxID)

	if t.vtxReqs.Len() == 0 {
		for _, txID := range t.missingTxs.List() {
			t.txBlocked.Abandon(txID)
		}
		t.missingTxs.Clear()
	}

	// Track performance statistics
	t.numVtxRequests.Set(float64(t.vtxReqs.Len()))
	t.numTxRequests.Set(float64(t.missingTxs.Len()))
}

// PullQuery implements the Engine interface
func (t *Transitive) PullQuery(vdr ids.ShortID, requestID uint32, vtxID ids.ID) {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("Dropping PullQuery for %s due to bootstrapping", vtxID)
		return
	}

	c := &convincer{
		consensus: t.Consensus,
		sender:    t.Config.Sender,
		vdr:       vdr,
		requestID: requestID,
	}

	if !t.reinsertFrom(vdr, vtxID) {
		c.deps.Add(vtxID)
	}

	t.vtxBlocked.Register(c)
}

// PushQuery implements the Engine interface
func (t *Transitive) PushQuery(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("Dropping PushQuery for %s due to bootstrapping", vtxID)
		return
	}

	vtx, err := t.Config.State.ParseVertex(vtxBytes)
	if err != nil {
		t.Config.Context.Log.Warn("ParseVertex failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: vtxBytes})
		return
	}
	t.insertFrom(vdr, vtx)

	t.PullQuery(vdr, requestID, vtx.ID())
}

// Chits implements the Engine interface
func (t *Transitive) Chits(vdr ids.ShortID, requestID uint32, votes ids.Set) {
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("Dropping Chits due to bootstrapping")
		return
	}

	v := &voter{
		t:         t,
		vdr:       vdr,
		requestID: requestID,
		response:  votes,
	}
	voteList := votes.List()
	for _, vote := range voteList {
		if !t.reinsertFrom(vdr, vote) {
			v.deps.Add(vote)
		}
	}

	t.vtxBlocked.Register(v)
}

// QueryFailed implements the Engine interface
func (t *Transitive) QueryFailed(vdr ids.ShortID, requestID uint32) {
	t.Chits(vdr, requestID, ids.Set{})
}

// Notify implements the Engine interface
func (t *Transitive) Notify(msg common.Message) {
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("Dropping Notify due to bootstrapping")
		return
	}

	switch msg {
	case common.PendingTxs:
		txs := t.Config.VM.PendingTxs()
		t.batch(txs, false /*=force*/, false /*=empty*/)
	}
}

func (t *Transitive) repoll() {
	if len(t.polls.m) >= t.Params.ConcurrentRepolls {
		return
	}

	txs := t.Config.VM.PendingTxs()
	t.batch(txs, false /*=force*/, true /*=empty*/)

	for i := len(t.polls.m); i < t.Params.ConcurrentRepolls; i++ {
		t.batch(nil, false /*=force*/, true /*=empty*/)
	}
}

func (t *Transitive) reinsertFrom(vdr ids.ShortID, vtxID ids.ID) bool {
	vtx, err := t.Config.State.GetVertex(vtxID)
	if err != nil {
		t.sendRequest(vdr, vtxID)
		return false
	}
	return t.insertFrom(vdr, vtx)
}

func (t *Transitive) insertFrom(vdr ids.ShortID, vtx avalanche.Vertex) bool {
	issued := true
	vts := []avalanche.Vertex{vtx}
	for len(vts) > 0 {
		vtx := vts[0]
		vts = vts[1:]

		if t.Consensus.VertexIssued(vtx) {
			continue
		}
		if t.pending.Contains(vtx.ID()) {
			issued = false
			continue
		}

		for _, parent := range vtx.Parents() {
			if !parent.Status().Fetched() {
				t.sendRequest(vdr, parent.ID())
				issued = false
			} else {
				vts = append(vts, parent)
			}
		}

		t.insert(vtx)
	}
	return issued
}

func (t *Transitive) insert(vtx avalanche.Vertex) {
	vtxID := vtx.ID()

	t.pending.Add(vtxID)
	t.vtxReqs.RemoveAny(vtxID)

	i := &issuer{
		t:   t,
		vtx: vtx,
	}

	for _, parent := range vtx.Parents() {
		if !t.Consensus.VertexIssued(parent) {
			i.vtxDeps.Add(parent.ID())
		}
	}

	txs := vtx.Txs()

	txIDs := ids.Set{}
	for _, tx := range txs {
		txIDs.Add(tx.ID())
	}

	for _, tx := range txs {
		for _, dep := range tx.Dependencies() {
			depID := dep.ID()
			if !txIDs.Contains(depID) && !t.Consensus.TxIssued(dep) {
				t.missingTxs.Add(depID)
				i.txDeps.Add(depID)
			}
		}
	}

	t.Config.Context.Log.Verbo("Vertex: %s is blocking on %d vertices and %d transactions", vtxID, i.vtxDeps.Len(), i.txDeps.Len())

	t.vtxBlocked.Register(&vtxIssuer{i: i})
	t.txBlocked.Register(&txIssuer{i: i})

	if t.vtxReqs.Len() == 0 {
		for _, txID := range t.missingTxs.List() {
			t.txBlocked.Abandon(txID)
		}
		t.missingTxs.Clear()
	}

	// Track performance statistics
	t.numVtxRequests.Set(float64(t.vtxReqs.Len()))
	t.numTxRequests.Set(float64(t.missingTxs.Len()))
	t.numBlockedVtx.Set(float64(t.pending.Len()))
}

func (t *Transitive) batch(txs []snowstorm.Tx, force, empty bool) {
	batch := []snowstorm.Tx(nil)
	issuedTxs := ids.Set{}
	consumed := ids.Set{}
	issued := false
	for _, tx := range txs {
		inputs := tx.InputIDs()
		overlaps := consumed.Overlaps(inputs)
		if len(batch) >= t.Params.BatchSize || (force && overlaps) {
			t.issueBatch(batch)
			batch = nil
			consumed.Clear()
			issued = true
			overlaps = false
		}

		// Force allows for a conflict to be issued
		if txID := tx.ID(); !overlaps && !issuedTxs.Contains(txID) && (force || t.Consensus.IsVirtuous(tx)) && !tx.Status().Decided() {
			batch = append(batch, tx)
			issuedTxs.Add(txID)
			consumed.Union(inputs)
		}
	}

	if len(batch) > 0 {
		t.issueBatch(batch)
	} else if empty && !issued {
		t.issueRepoll()
	}
}

func (t *Transitive) issueRepoll() {
	preferredIDs := t.Consensus.Preferences().List()
	numPreferredIDs := len(preferredIDs)
	if numPreferredIDs == 0 {
		t.Config.Context.Log.Error("Re-query attempt was dropped due to no pending vertices")
		return
	}

	sampler := random.Uniform{N: len(preferredIDs)}
	vtxID := preferredIDs[sampler.Sample()]

	p := t.Consensus.Parameters()
	vdrs := t.Config.Validators.Sample(p.K) // Validators to sample

	vdrSet := ids.ShortSet{} // Validators to sample repr. as a set
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	t.RequestID++
	if numVdrs := len(vdrs); numVdrs == p.K && t.polls.Add(t.RequestID, vdrSet.Len()) {
		t.Config.Sender.PullQuery(vdrSet, t.RequestID, vtxID)
	} else if numVdrs < p.K {
		t.Config.Context.Log.Error("Re-query for %s was dropped due to an insufficient number of validators", vtxID)
	}
}

func (t *Transitive) issueBatch(txs []snowstorm.Tx) {
	t.Config.Context.Log.Verbo("Batching %d transactions into a new vertex", len(txs))

	virtuousIDs := t.Consensus.Virtuous().List()
	sampler := random.Uniform{N: len(virtuousIDs)}
	parentIDs := ids.Set{}
	for i := 0; i < t.Params.Parents && sampler.CanSample(); i++ {
		parentIDs.Add(virtuousIDs[sampler.Sample()])
	}

	if vtx, err := t.Config.State.BuildVertex(parentIDs, txs); err == nil {
		t.insert(vtx)
	} else {
		t.Config.Context.Log.Warn("Error building new vertex with %d parents and %d transactions", len(parentIDs), len(txs))
	}
}

func (t *Transitive) sendRequest(vdr ids.ShortID, vtxID ids.ID) {
	if t.vtxReqs.Contains(vtxID) {
		t.Config.Context.Log.Debug("Not requesting a vertex because we have recently sent a request")
		return
	}

	t.RequestID++

	t.vtxReqs.Add(vdr, t.RequestID, vtxID)
	t.Config.Sender.Get(vdr, t.RequestID, vtxID)

	t.numVtxRequests.Set(float64(t.vtxReqs.Len())) // Tracks performance statistics
}
