// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"sync/atomic"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/network"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/avalanche/poll"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/events"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/random"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	// TODO define this constant in one place rather than here and in snowman
	// Max containers size in a MultiPut message
	maxContainersLen = int(4 * network.DefaultMaxMessageSize / 5)
)

// Transitive implements the Engine interface by attempting to fetch all
// transitive dependencies.
type Transitive struct {
	Config
	bootstrapper

	polls poll.Set // track people I have asked for their preference

	// vtxReqs prevents asking validators for the same vertex
	vtxReqs common.Requests

	// missingTxs tracks transaction that are missing
	missingTxs, pending ids.Set

	// vtxBlocked tracks operations that are blocked on vertices
	// txBlocked tracks operations that are blocked on transactions
	vtxBlocked, txBlocked events.Blocker

	bootstrapped       bool
	atomicBootstrapped *uint32

	errs wrappers.Errs
}

// Initialize implements the Engine interface
func (t *Transitive) Initialize(config Config) error {
	config.Context.Log.Info("Initializing consensus engine")

	t.Config = config
	t.metrics.Initialize(config.Context.Log, config.Params.Namespace, config.Params.Metrics)

	t.onFinished = t.finishBootstrapping
	t.atomicBootstrapped = new(uint32)

	factory := poll.NewEarlyTermNoTraversalFactory(int(config.Params.Alpha))
	t.polls = poll.NewSet(factory,
		config.Context.Log,
		config.Params.Namespace,
		config.Params.Metrics,
	)

	return t.bootstrapper.Initialize(config.BootstrapConfig)
}

func (t *Transitive) finishBootstrapping() error {
	// Load the vertices that were last saved as the accepted frontier
	frontier := []avalanche.Vertex(nil)
	for _, vtxID := range t.Config.State.Edge() {
		if vtx, err := t.Config.State.GetVertex(vtxID); err == nil {
			frontier = append(frontier, vtx)
		} else {
			t.Config.Context.Log.Error("vertex %s failed to be loaded from the frontier with %s", vtxID, err)
		}
	}
	t.Consensus.Initialize(t.Config.Context, t.Params, frontier)
	t.bootstrapped = true
	atomic.StoreUint32(t.atomicBootstrapped, 1)

	t.Config.Context.Log.Info("bootstrapping finished with %d vertices in the accepted frontier", len(frontier))
	return nil
}

// Gossip implements the Engine interface
func (t *Transitive) Gossip() error {
	edge := t.Config.State.Edge()
	if len(edge) == 0 {
		t.Config.Context.Log.Verbo("dropping gossip request as no vertices have been accepted")
		return nil
	}

	sampler := random.Uniform{N: len(edge)}
	vtxID := edge[sampler.Sample()]
	vtx, err := t.Config.State.GetVertex(vtxID)
	if err != nil {
		t.Config.Context.Log.Warn("dropping gossip request as %s couldn't be loaded due to: %s", vtxID, err)
		return nil
	}

	t.Config.Context.Log.Verbo("gossiping %s as accepted to the network", vtxID)
	t.Config.Sender.Gossip(vtxID, vtx.Bytes())
	return nil
}

// Shutdown implements the Engine interface
func (t *Transitive) Shutdown() error {
	t.Config.Context.Log.Info("shutting down consensus engine")
	return t.Config.VM.Shutdown()
}

// Context implements the Engine interface
func (t *Transitive) Context() *snow.Context { return t.Config.Context }

// Get implements the Engine interface
func (t *Transitive) Get(vdr ids.ShortID, requestID uint32, vtxID ids.ID) error {
	// If this engine has access to the requested vertex, provide it
	if vtx, err := t.Config.State.GetVertex(vtxID); err == nil {
		t.Config.Sender.Put(vdr, requestID, vtxID, vtx.Bytes())
	}
	return nil
}

// GetAncestors implements the Engine interface
func (t *Transitive) GetAncestors(vdr ids.ShortID, requestID uint32, vtxID ids.ID) error {
	startTime := time.Now()
	t.Config.Context.Log.Verbo("GetAncestors(%s, %d, %s) called", vdr, requestID, vtxID)
	vertex, err := t.Config.State.GetVertex(vtxID)
	if err != nil || vertex.Status() == choices.Unknown {
		t.Config.Context.Log.Verbo("dropping getAncestors")
		return nil // Don't have the requested vertex. Drop message.
	}

	queue := make([]avalanche.Vertex, 1, common.MaxContainersPerMultiPut) // for BFS
	queue[0] = vertex
	ancestorsBytesLen := 0                                               // length, in bytes, of vertex and its ancestors
	ancestorsBytes := make([][]byte, 0, common.MaxContainersPerMultiPut) // vertex and its ancestors in BFS order
	visited := ids.Set{}                                                 // IDs of vertices that have been in queue before
	visited.Add(vertex.ID())

	for len(ancestorsBytes) < common.MaxContainersPerMultiPut && len(queue) > 0 && time.Since(startTime) < common.MaxTimeFetchingAncestors {
		var vtx avalanche.Vertex
		vtx, queue = queue[0], queue[1:] // pop
		vtxBytes := vtx.Bytes()
		// Ensure response size isn't too large. Include wrappers.IntLen because the size of the message
		// is included with each container, and the size is repr. by an int.
		if newLen := wrappers.IntLen + ancestorsBytesLen + len(vtxBytes); newLen < maxContainersLen {
			ancestorsBytes = append(ancestorsBytes, vtxBytes)
			ancestorsBytesLen = newLen
		} else { // reached maximum response size
			break
		}
		parents, err := vtx.Parents()
		if err != nil {
			return err
		}
		for _, parent := range parents {
			if parent.Status() == choices.Unknown { // Don't have this vertex;ignore
				continue
			}
			if parentID := parent.ID(); !visited.Contains(parentID) { // If already visited, ignore
				queue = append(queue, parent)
				visited.Add(parentID)
			}
		}
	}

	t.Config.Sender.MultiPut(vdr, requestID, ancestorsBytes)
	return nil
}

// Put implements the Engine interface
func (t *Transitive) Put(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) error {
	t.Config.Context.Log.Verbo("Put(%s, %d, %s) called", vdr, requestID, vtxID)

	if !t.bootstrapped { // Bootstrapping unfinished --> didn't call Get --> this message is invalid
		if requestID == network.GossipMsgRequestID {
			t.Config.Context.Log.Verbo("dropping gossip Put(%s, %d, %s) due to bootstrapping", vdr, requestID, vtxID)
		} else {
			t.Config.Context.Log.Debug("dropping Put(%s, %d, %s) due to bootstrapping", vdr, requestID, vtxID)
		}
		return nil
	}

	vtx, err := t.Config.State.ParseVertex(vtxBytes)
	if err != nil {
		t.Config.Context.Log.Debug("failed to parse vertex %s due to: %s", vtxID, err)
		t.Config.Context.Log.Verbo("vertex:\n%s", formatting.DumpBytes{Bytes: vtxBytes})
		return t.GetFailed(vdr, requestID)
	}
	_, err = t.insertFrom(vdr, vtx)
	return err
}

// GetFailed implements the Engine interface
func (t *Transitive) GetFailed(vdr ids.ShortID, requestID uint32) error {
	if !t.bootstrapped { // Bootstrapping unfinished --> didn't call Get --> this message is invalid
		t.Config.Context.Log.Debug("dropping GetFailed(%s, %d) due to bootstrapping", vdr, requestID)
		return nil
	}

	vtxID, ok := t.vtxReqs.Remove(vdr, requestID)
	if !ok {
		t.Config.Context.Log.Debug("GetFailed(%s, %d) called without having sent corresponding Get", vdr, requestID)
		return nil
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
	return t.errs.Err
}

// PullQuery implements the Engine interface
func (t *Transitive) PullQuery(vdr ids.ShortID, requestID uint32, vtxID ids.ID) error {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping PullQuery(%s, %d, %s) due to bootstrapping", vdr, requestID, vtxID)
		return nil
	}

	c := &convincer{
		consensus: t.Consensus,
		sender:    t.Config.Sender,
		vdr:       vdr,
		requestID: requestID,
		errs:      &t.errs,
	}

	added, err := t.reinsertFrom(vdr, vtxID)
	if err != nil {
		return err
	}

	if !added {
		c.deps.Add(vtxID)
	}

	t.vtxBlocked.Register(c)
	return t.errs.Err
}

// PushQuery implements the Engine interface
func (t *Transitive) PushQuery(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtxBytes []byte) error {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping PushQuery(%s, %d, %s) due to bootstrapping", vdr, requestID, vtxID)
		return nil
	}

	vtx, err := t.Config.State.ParseVertex(vtxBytes)
	if err != nil {
		t.Config.Context.Log.Debug("failed to parse vertex %s due to: %s", vtxID, err)
		t.Config.Context.Log.Verbo("vertex:\n%s", formatting.DumpBytes{Bytes: vtxBytes})
		return nil
	}

	if _, err := t.insertFrom(vdr, vtx); err != nil {
		return err
	}

	return t.PullQuery(vdr, requestID, vtx.ID())
}

// Chits implements the Engine interface
func (t *Transitive) Chits(vdr ids.ShortID, requestID uint32, votes ids.Set) error {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping Chits(%s, %d) due to bootstrapping", vdr, requestID)
		return nil
	}

	v := &voter{
		t:         t,
		vdr:       vdr,
		requestID: requestID,
		response:  votes,
	}
	voteList := votes.List()
	for _, vote := range voteList {
		if added, err := t.reinsertFrom(vdr, vote); err != nil {
			return err
		} else if !added {
			v.deps.Add(vote)
		}
	}

	t.vtxBlocked.Register(v)
	return t.errs.Err
}

// QueryFailed implements the Engine interface
func (t *Transitive) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	return t.Chits(vdr, requestID, ids.Set{})
}

// Notify implements the Engine interface
func (t *Transitive) Notify(msg common.Message) error {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping Notify due to bootstrapping")
		return nil
	}

	switch msg {
	case common.PendingTxs:
		txs := t.Config.VM.PendingTxs()
		return t.batch(txs, false /*=force*/, false /*=empty*/)
	}
	return nil
}

func (t *Transitive) repoll() error {
	if t.polls.Len() >= t.Params.ConcurrentRepolls || t.errs.Errored() {
		return nil
	}

	txs := t.Config.VM.PendingTxs()
	if err := t.batch(txs, false /*=force*/, true /*=empty*/); err != nil {
		return err
	}

	for i := t.polls.Len(); i < t.Params.ConcurrentRepolls; i++ {
		if err := t.batch(nil, false /*=force*/, true /*=empty*/); err != nil {
			return err
		}
	}
	return nil
}

func (t *Transitive) reinsertFrom(vdr ids.ShortID, vtxID ids.ID) (bool, error) {
	vtx, err := t.Config.State.GetVertex(vtxID)
	if err != nil {
		t.sendRequest(vdr, vtxID)
		return false, nil
	}
	return t.insertFrom(vdr, vtx)
}

func (t *Transitive) insertFrom(vdr ids.ShortID, vtx avalanche.Vertex) (bool, error) {
	issued := true
	vertexHeap := newMaxVertexHeap()
	vertexHeap.Push(vtx)
	for vertexHeap.Len() > 0 {
		vtx := vertexHeap.Pop()

		if t.Consensus.VertexIssued(vtx) {
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
		for _, parent := range parents {
			if !parent.Status().Fetched() {
				t.sendRequest(vdr, parent.ID())
				issued = false
			} else {
				vertexHeap.Push(parent)
			}
		}

		if err := t.insert(vtx); err != nil {
			return false, err
		}
	}
	return issued, nil
}

func (t *Transitive) insert(vtx avalanche.Vertex) error {
	vtxID := vtx.ID()

	t.pending.Add(vtxID)
	t.vtxReqs.RemoveAny(vtxID)

	i := &issuer{
		t:   t,
		vtx: vtx,
	}

	parents, err := vtx.Parents()
	if err != nil {
		return errGetTx
	}
	for _, parent := range parents {
		if !t.Consensus.VertexIssued(parent) {
			i.vtxDeps.Add(parent.ID())
		}
	}

	txs, err := vtx.Txs()
	if err != nil {
		return err
	}

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

	t.Config.Context.Log.Verbo("vertex %s is blocking on %d vertices and %d transactions", vtxID, i.vtxDeps.Len(), i.txDeps.Len())

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
	t.numPendingVtx.Set(float64(t.pending.Len()))
	return t.errs.Err
}

func (t *Transitive) batch(txs []snowstorm.Tx, force, empty bool) error {
	batch := make([]snowstorm.Tx, 0, t.Params.BatchSize)
	issuedTxs := ids.Set{}
	consumed := ids.Set{}
	issued := false
	orphans := t.Consensus.Orphans()
	for _, tx := range txs {
		inputs := tx.InputIDs()
		overlaps := consumed.Overlaps(inputs)
		if len(batch) >= t.Params.BatchSize || (force && overlaps) {
			t.issueBatch(batch)
			batch = make([]snowstorm.Tx, 0, t.Params.BatchSize)
			consumed.Clear()
			issued = true
			overlaps = false
		}

		if txID := tx.ID(); !overlaps && // should never allow conflicting txs in the same vertex
			!issuedTxs.Contains(txID) && // shouldn't issue duplicated transactions to the same vertex
			(force || t.Consensus.IsVirtuous(tx)) && // force allows for a conflict to be issued
			(!t.Consensus.TxIssued(tx) || orphans.Contains(txID)) { // should only reissued orphaned txs
			batch = append(batch, tx)
			issuedTxs.Add(txID)
			consumed.Union(inputs)
		}
	}

	if len(batch) > 0 {
		return t.issueBatch(batch)
	} else if empty && !issued {
		t.issueRepoll()
	}
	return nil
}

func (t *Transitive) issueRepoll() {
	preferredIDs := t.Consensus.Preferences().List()
	numPreferredIDs := len(preferredIDs)
	if numPreferredIDs == 0 {
		t.Config.Context.Log.Error("re-query attempt was dropped due to no pending vertices")
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

	vdrCopy := ids.ShortSet{}
	vdrCopy.Union((vdrSet))

	t.RequestID++
	if numVdrs := len(vdrs); numVdrs == p.K && t.polls.Add(t.RequestID, vdrCopy) {
		t.Config.Sender.PullQuery(vdrSet, t.RequestID, vtxID)
	} else if numVdrs < p.K {
		t.Config.Context.Log.Error("re-query for %s was dropped due to an insufficient number of validators", vtxID)
	}
}

func (t *Transitive) issueBatch(txs []snowstorm.Tx) error {
	t.Config.Context.Log.Verbo("batching %d transactions into a new vertex", len(txs))

	virtuousIDs := t.Consensus.Virtuous().List()
	sampler := random.Uniform{N: len(virtuousIDs)}
	parentIDs := ids.Set{}
	for i := 0; i < t.Params.Parents && sampler.CanSample(); i++ {
		parentIDs.Add(virtuousIDs[sampler.Sample()])
	}

	vtx, err := t.Config.State.BuildVertex(parentIDs, txs)
	if err != nil {
		t.Config.Context.Log.Warn("error building new vertex with %d parents and %d transactions", len(parentIDs), len(txs))
		return nil
	}
	return t.insert(vtx)
}

func (t *Transitive) sendRequest(vdr ids.ShortID, vtxID ids.ID) {
	if t.vtxReqs.Contains(vtxID) {
		t.Config.Context.Log.Debug("not requesting a vertex because we have recently sent a request")
		return
	}

	t.RequestID++

	t.vtxReqs.Add(vdr, t.RequestID, vtxID)
	t.Config.Sender.Get(vdr, t.RequestID, vtxID)

	t.numVtxRequests.Set(float64(t.vtxReqs.Len())) // Tracks performance statistics
}

// IsBootstrapped returns true iff this chain is done bootstrapping
func (t *Transitive) IsBootstrapped() bool {
	return atomic.LoadUint32(t.atomicBootstrapped) > 0
}
