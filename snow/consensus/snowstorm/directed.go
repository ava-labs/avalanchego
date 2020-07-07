// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/events"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/wrappers"
)

// DirectedFactory implements Factory by returning a directed struct
type DirectedFactory struct{}

// New implements Factory
func (DirectedFactory) New() Consensus { return &Directed{} }

// Directed is an implementation of a multi-color, non-transitive, snowball
// instance
type Directed struct {
	metrics

	ctx    *snow.Context
	params snowball.Parameters

	// Each element of preferences is the ID of a transaction that is preferred.
	// That is, each transaction has no out edges
	preferences ids.Set

	// Each element of virtuous is the ID of a transaction that is virtuous.
	// That is, each transaction that has no incident edges
	virtuous ids.Set

	// Each element is in the virtuous set and is still being voted on
	virtuousVoting ids.Set

	// Key: UTXO ID
	// Value: IDs of transactions that consume the UTXO specified in the key
	spends map[[32]byte]ids.Set

	// Key: Transaction ID
	// Value: Node that represents this transaction in the conflict graph
	nodes map[[32]byte]*flatNode

	// Keep track of whether dependencies have been accepted or rejected
	pendingAccept, pendingReject events.Blocker

	// Number of times RecordPoll has been called
	currentVote int

	errs wrappers.Errs
}

type flatNode struct {
	bias, confidence, lastVote int

	pendingAccept, accepted, rogue bool
	ins, outs                      ids.Set

	tx Tx
}

// Initialize implements the Consensus interface
func (dg *Directed) Initialize(ctx *snow.Context, params snowball.Parameters) {
	ctx.Log.AssertDeferredNoError(params.Valid)

	dg.ctx = ctx
	dg.params = params

	if err := dg.metrics.Initialize(ctx.Log, params.Namespace, params.Metrics); err != nil {
		dg.ctx.Log.Error("%s", err)
	}

	dg.spends = make(map[[32]byte]ids.Set)
	dg.nodes = make(map[[32]byte]*flatNode)
}

// Parameters implements the Snowstorm interface
func (dg *Directed) Parameters() snowball.Parameters { return dg.params }

// IsVirtuous implements the Consensus interface
func (dg *Directed) IsVirtuous(tx Tx) bool {
	id := tx.ID()
	if node, exists := dg.nodes[id.Key()]; exists {
		return !node.rogue
	}
	for _, input := range tx.InputIDs().List() {
		if _, exists := dg.spends[input.Key()]; exists {
			return false
		}
	}
	return true
}

// Conflicts implements the Consensus interface
func (dg *Directed) Conflicts(tx Tx) ids.Set {
	id := tx.ID()
	conflicts := ids.Set{}

	if node, exists := dg.nodes[id.Key()]; exists {
		conflicts.Union(node.ins)
		conflicts.Union(node.outs)
	} else {
		for _, input := range tx.InputIDs().List() {
			if spends, exists := dg.spends[input.Key()]; exists {
				conflicts.Union(spends)
			}
		}
		conflicts.Remove(id)
	}

	return conflicts
}

// Add implements the Consensus interface
func (dg *Directed) Add(tx Tx) error {
	if dg.Issued(tx) {
		return nil // Already inserted
	}

	txID := tx.ID()
	bytes := tx.Bytes()

	dg.ctx.DecisionDispatcher.Issue(dg.ctx.ChainID, txID, bytes)
	inputs := tx.InputIDs()
	// If there are no inputs, Tx is vacuously accepted
	if inputs.Len() == 0 {
		if err := tx.Accept(); err != nil {
			return err
		}
		dg.ctx.DecisionDispatcher.Accept(dg.ctx.ChainID, txID, bytes)
		dg.metrics.Issued(txID)
		dg.metrics.Accepted(txID)
		return nil
	}

	fn := &flatNode{tx: tx}

	// Note: Below, for readability, we sometimes say "transaction" when we actually mean
	// "the flatNode representing a transaction."
	// For each UTXO input to Tx:
	// * Get all transactions that consume that UTXO
	// * Add edges from Tx to those transactions in the conflict graph
	// * Mark those transactions as rogue
	for _, inputID := range inputs.List() {
		inputKey := inputID.Key()
		spends := dg.spends[inputKey] // Transactions spending this UTXO

		// Add edges to conflict graph
		fn.outs.Union(spends)

		// Mark transactions conflicting with Tx as rogue
		for _, conflictID := range spends.List() {
			conflictKey := conflictID.Key()
			conflict := dg.nodes[conflictKey]

			dg.virtuous.Remove(conflictID)
			dg.virtuousVoting.Remove(conflictID)

			conflict.rogue = true
			conflict.ins.Add(txID)

			dg.nodes[conflictKey] = conflict
		}
		// Add Tx to list of transactions consuming UTXO whose ID is id
		spends.Add(txID)
		dg.spends[inputKey] = spends
	}
	fn.rogue = fn.outs.Len() != 0 // Mark this transaction as rogue if it has conflicts

	// Add the node representing Tx to the node set
	dg.nodes[txID.Key()] = fn
	if !fn.rogue {
		// I'm not rogue
		dg.virtuous.Add(txID)
		dg.virtuousVoting.Add(txID)

		// If I'm not rogue, I must be preferred
		dg.preferences.Add(txID)
	}
	dg.metrics.Issued(txID)

	// Tx can be accepted only if the transactions it depends on are also accepted
	// If any transactions that Tx depends on are rejected, reject Tx
	toReject := &directedRejector{
		dg: dg,
		fn: fn,
	}
	for _, dependency := range tx.Dependencies() {
		if !dependency.Status().Decided() {
			toReject.deps.Add(dependency.ID())
		}
	}
	dg.pendingReject.Register(toReject)
	return dg.errs.Err
}

// Issued implements the Consensus interface
func (dg *Directed) Issued(tx Tx) bool {
	if tx.Status().Decided() {
		return true
	}
	_, ok := dg.nodes[tx.ID().Key()]
	return ok
}

// Virtuous implements the Consensus interface
func (dg *Directed) Virtuous() ids.Set { return dg.virtuous }

// Preferences implements the Consensus interface
func (dg *Directed) Preferences() ids.Set { return dg.preferences }

// RecordPoll implements the Consensus interface
func (dg *Directed) RecordPoll(votes ids.Bag) (bool, error) {
	dg.currentVote++
	changed := false

	votes.SetThreshold(dg.params.Alpha)
	threshold := votes.Threshold() // Each element is ID of transaction preferred by >= Alpha poll respondents
	for _, toInc := range threshold.List() {
		incKey := toInc.Key()
		fn, exist := dg.nodes[incKey]
		if !exist {
			// Votes for decided consumers are ignored
			continue
		}

		if fn.lastVote+1 != dg.currentVote {
			fn.confidence = 0
		}
		fn.lastVote = dg.currentVote

		dg.ctx.Log.Verbo("Increasing (bias, confidence) of %s from (%d, %d) to (%d, %d)",
			toInc, fn.bias, fn.confidence, fn.bias+1, fn.confidence+1)

		fn.bias++
		fn.confidence++

		if !fn.pendingAccept &&
			((!fn.rogue && fn.confidence >= dg.params.BetaVirtuous) ||
				fn.confidence >= dg.params.BetaRogue) {
			dg.deferAcceptance(fn)
			if dg.errs.Errored() {
				return changed, dg.errs.Err
			}
		}
		if !fn.accepted {
			changed = dg.redirectEdges(fn) || changed
		} else {
			changed = true
		}
	}
	return changed, dg.errs.Err
}

// Quiesce implements the Consensus interface
func (dg *Directed) Quiesce() bool {
	numVirtuous := dg.virtuousVoting.Len()
	dg.ctx.Log.Verbo("Conflict graph has %d voting virtuous transactions and %d transactions",
		numVirtuous, len(dg.nodes))
	return numVirtuous == 0
}

// Finalized implements the Consensus interface
func (dg *Directed) Finalized() bool {
	numNodes := len(dg.nodes)
	dg.ctx.Log.Verbo("Conflict graph has %d pending transactions", numNodes)
	return numNodes == 0
}

func (dg *Directed) String() string {
	nodes := []*flatNode{}
	for _, fn := range dg.nodes {
		nodes = append(nodes, fn)
	}
	sortFlatNodes(nodes)

	sb := strings.Builder{}

	sb.WriteString("DG(")

	format := fmt.Sprintf(
		"\n    Choice[%s] = ID: %%50s Confidence: %s Bias: %%d",
		formatting.IntFormat(len(dg.nodes)-1),
		formatting.IntFormat(dg.params.BetaRogue-1))

	for i, fn := range nodes {
		confidence := fn.confidence
		if fn.lastVote != dg.currentVote {
			confidence = 0
		}
		sb.WriteString(fmt.Sprintf(format,
			i, fn.tx.ID(), confidence, fn.bias))
	}

	if len(nodes) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(")")

	return sb.String()
}

func (dg *Directed) deferAcceptance(fn *flatNode) {
	fn.pendingAccept = true

	toAccept := &directedAccepter{
		dg: dg,
		fn: fn,
	}
	for _, dependency := range fn.tx.Dependencies() {
		if !dependency.Status().Decided() {
			toAccept.deps.Add(dependency.ID())
		}
	}

	dg.virtuousVoting.Remove(fn.tx.ID())
	dg.pendingAccept.Register(toAccept)
}

func (dg *Directed) reject(ids ...ids.ID) error {
	for _, conflict := range ids {
		conflictKey := conflict.Key()
		conf := dg.nodes[conflictKey]
		delete(dg.nodes, conflictKey)

		dg.preferences.Remove(conflict)

		// remove the edge between this node and all its neighbors
		dg.removeConflict(conflict, conf.ins.List()...)
		dg.removeConflict(conflict, conf.outs.List()...)

		// Mark it as rejected
		if err := conf.tx.Reject(); err != nil {
			return err
		}
		dg.ctx.DecisionDispatcher.Reject(dg.ctx.ChainID, conf.tx.ID(), conf.tx.Bytes())
		dg.metrics.Rejected(conflict)

		dg.pendingAccept.Abandon(conflict)
		dg.pendingReject.Fulfill(conflict)
	}
	return nil
}

func (dg *Directed) redirectEdges(fn *flatNode) bool {
	changed := false
	for _, conflictID := range fn.outs.List() {
		changed = dg.redirectEdge(fn, conflictID) || changed
	}
	return changed
}

// Set the confidence of all conflicts to 0
// Change the direction of edges if needed
func (dg *Directed) redirectEdge(fn *flatNode, conflictID ids.ID) bool {
	nodeID := fn.tx.ID()
	conflict := dg.nodes[conflictID.Key()]
	if fn.bias <= conflict.bias {
		return false
	}

	// TODO: why is this confidence reset here? It should already be reset
	// implicitly by the lack of a timestamp increase.
	conflict.confidence = 0

	// Change the edge direction
	conflict.ins.Remove(nodeID)
	conflict.outs.Add(nodeID)
	dg.preferences.Remove(conflictID) // This consumer now has an out edge

	fn.ins.Add(conflictID)
	fn.outs.Remove(conflictID)
	if fn.outs.Len() == 0 {
		// If I don't have out edges, I'm preferred
		dg.preferences.Add(nodeID)
	}
	return true
}

func (dg *Directed) removeConflict(id ids.ID, ids ...ids.ID) {
	for _, neighborID := range ids {
		neighborKey := neighborID.Key()
		// If the neighbor doesn't exist, they may have already been rejected
		if neighbor, exists := dg.nodes[neighborKey]; exists {
			neighbor.ins.Remove(id)
			neighbor.outs.Remove(id)

			if neighbor.outs.Len() == 0 {
				// Make sure to mark the neighbor as preferred if needed
				dg.preferences.Add(neighborID)
			}

			dg.nodes[neighborKey] = neighbor
		}
	}
}

type directedAccepter struct {
	dg       *Directed
	deps     ids.Set
	rejected bool
	fn       *flatNode
}

func (a *directedAccepter) Dependencies() ids.Set { return a.deps }

func (a *directedAccepter) Fulfill(id ids.ID) {
	a.deps.Remove(id)
	a.Update()
}

func (a *directedAccepter) Abandon(id ids.ID) { a.rejected = true }

func (a *directedAccepter) Update() {
	// If I was rejected or I am still waiting on dependencies to finish do nothing.
	if a.rejected || a.deps.Len() != 0 || a.dg.errs.Errored() {
		return
	}

	id := a.fn.tx.ID()
	delete(a.dg.nodes, id.Key())

	for _, inputID := range a.fn.tx.InputIDs().List() {
		delete(a.dg.spends, inputID.Key())
	}
	a.dg.virtuous.Remove(id)
	a.dg.preferences.Remove(id)

	// Reject the conflicts
	if err := a.dg.reject(a.fn.ins.List()...); err != nil {
		a.dg.errs.Add(err)
		return
	}
	// Should normally be empty
	if err := a.dg.reject(a.fn.outs.List()...); err != nil {
		a.dg.errs.Add(err)
		return
	}

	// Mark it as accepted
	if err := a.fn.tx.Accept(); err != nil {
		a.dg.errs.Add(err)
		return
	}
	a.fn.accepted = true
	a.dg.ctx.DecisionDispatcher.Accept(a.dg.ctx.ChainID, id, a.fn.tx.Bytes())
	a.dg.metrics.Accepted(id)

	a.dg.pendingAccept.Fulfill(id)
	a.dg.pendingReject.Abandon(id)
}

// directedRejector implements Blockable
type directedRejector struct {
	dg       *Directed
	deps     ids.Set
	rejected bool // true if the transaction represented by fn has been rejected
	fn       *flatNode
}

func (r *directedRejector) Dependencies() ids.Set { return r.deps }

func (r *directedRejector) Fulfill(id ids.ID) {
	if r.rejected || r.dg.errs.Errored() {
		return
	}
	r.rejected = true
	r.dg.errs.Add(r.dg.reject(r.fn.tx.ID()))
}

func (*directedRejector) Abandon(id ids.ID) {}

func (*directedRejector) Update() {}

type sortFlatNodeData []*flatNode

func (fnd sortFlatNodeData) Less(i, j int) bool {
	return bytes.Compare(
		fnd[i].tx.ID().Bytes(),
		fnd[j].tx.ID().Bytes()) == -1
}
func (fnd sortFlatNodeData) Len() int      { return len(fnd) }
func (fnd sortFlatNodeData) Swap(i, j int) { fnd[j], fnd[i] = fnd[i], fnd[j] }

func sortFlatNodes(nodes []*flatNode) { sort.Sort(sortFlatNodeData(nodes)) }
