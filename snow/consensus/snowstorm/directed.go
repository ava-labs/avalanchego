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
	"github.com/ava-labs/gecko/utils/formatting"
)

// DirectedFactory implements Factory by returning a directed struct
type DirectedFactory struct{}

// New implements Factory
func (DirectedFactory) New() Consensus { return &Directed{} }

// Directed is an implementation of a multi-color, non-transitive, snowball
// instance
type Directed struct {
	common

	// Key: Transaction ID
	// Value: Node that represents this transaction in the conflict graph
	txs map[[32]byte]*directedTx

	// Key: UTXO ID
	// Value: IDs of transactions that consume the UTXO specified in the key
	utxos map[[32]byte]ids.Set
}

type directedTx struct {
	bias, confidence, lastVote int
	rogue                      bool

	pendingAccept, accepted bool
	ins, outs               ids.Set

	tx Tx
}

// Initialize implements the Consensus interface
func (dg *Directed) Initialize(ctx *snow.Context, params snowball.Parameters) {
	dg.common.Initialize(ctx, params)

	dg.utxos = make(map[[32]byte]ids.Set)
	dg.txs = make(map[[32]byte]*directedTx)
}

// IsVirtuous implements the Consensus interface
func (dg *Directed) IsVirtuous(tx Tx) bool {
	id := tx.ID()
	if node, exists := dg.txs[id.Key()]; exists {
		return !node.rogue
	}
	for _, input := range tx.InputIDs().List() {
		if _, exists := dg.utxos[input.Key()]; exists {
			return false
		}
	}
	return true
}

// Conflicts implements the Consensus interface
func (dg *Directed) Conflicts(tx Tx) ids.Set {
	id := tx.ID()
	conflicts := ids.Set{}

	if node, exists := dg.txs[id.Key()]; exists {
		conflicts.Union(node.ins)
		conflicts.Union(node.outs)
	} else {
		for _, input := range tx.InputIDs().List() {
			if spends, exists := dg.utxos[input.Key()]; exists {
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

	txNode := &directedTx{tx: tx}

	// For each UTXO input to Tx:
	// * Get all transactions that consume that UTXO
	// * Add edges from Tx to those transactions in the conflict graph
	// * Mark those transactions as rogue
	for _, inputID := range inputs.List() {
		inputKey := inputID.Key()
		spends := dg.utxos[inputKey] // Transactions spending this UTXO

		// Add edges to conflict graph
		txNode.outs.Union(spends)

		// Mark transactions conflicting with Tx as rogue
		for _, conflictID := range spends.List() {
			conflictKey := conflictID.Key()
			conflict := dg.txs[conflictKey]

			dg.virtuous.Remove(conflictID)
			dg.virtuousVoting.Remove(conflictID)

			conflict.rogue = true
			conflict.ins.Add(txID)

			dg.txs[conflictKey] = conflict
		}
		// Add Tx to list of transactions consuming UTXO whose ID is id
		spends.Add(txID)
		dg.utxos[inputKey] = spends
	}
	txNode.rogue = txNode.outs.Len() != 0 // Mark this transaction as rogue if it has conflicts

	// Add the node representing Tx to the node set
	dg.txs[txID.Key()] = txNode
	if !txNode.rogue {
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
		dg:     dg,
		txNode: txNode,
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
	_, ok := dg.txs[tx.ID().Key()]
	return ok
}

// RecordPoll implements the Consensus interface
func (dg *Directed) RecordPoll(votes ids.Bag) (bool, error) {
	dg.currentVote++
	changed := false

	votes.SetThreshold(dg.params.Alpha)
	threshold := votes.Threshold() // Each element is ID of transaction preferred by >= Alpha poll respondents
	for _, toInc := range threshold.List() {
		incKey := toInc.Key()
		txNode, exist := dg.txs[incKey]
		if !exist {
			// Votes for decided consumers are ignored
			continue
		}

		if txNode.lastVote+1 != dg.currentVote {
			txNode.confidence = 0
		}
		txNode.lastVote = dg.currentVote

		dg.ctx.Log.Verbo("Increasing (bias, confidence) of %s from (%d, %d) to (%d, %d)",
			toInc, txNode.bias, txNode.confidence, txNode.bias+1, txNode.confidence+1)

		txNode.bias++
		txNode.confidence++

		if !txNode.pendingAccept &&
			((!txNode.rogue && txNode.confidence >= dg.params.BetaVirtuous) ||
				txNode.confidence >= dg.params.BetaRogue) {
			dg.deferAcceptance(txNode)
			if dg.errs.Errored() {
				return changed, dg.errs.Err
			}
		}
		if !txNode.accepted {
			changed = dg.redirectEdges(txNode) || changed
		} else {
			changed = true
		}
	}
	return changed, dg.errs.Err
}

func (dg *Directed) String() string {
	nodes := make([]*directedTx, 0, len(dg.txs))
	for _, tx := range dg.txs {
		nodes = append(nodes, tx)
	}
	sortTxNodes(nodes)

	sb := strings.Builder{}

	sb.WriteString("DG(")

	format := fmt.Sprintf(
		"\n    Choice[%s] = ID: %%50s Confidence: %s Bias: %%d",
		formatting.IntFormat(len(dg.txs)-1),
		formatting.IntFormat(dg.params.BetaRogue-1))

	for i, txNode := range nodes {
		confidence := txNode.confidence
		if txNode.lastVote != dg.currentVote {
			confidence = 0
		}
		sb.WriteString(fmt.Sprintf(format,
			i, txNode.tx.ID(), confidence, txNode.bias))
	}

	if len(nodes) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(")")

	return sb.String()
}

func (dg *Directed) deferAcceptance(txNode *directedTx) {
	txNode.pendingAccept = true

	toAccept := &directedAccepter{
		dg:     dg,
		txNode: txNode,
	}
	for _, dependency := range txNode.tx.Dependencies() {
		if !dependency.Status().Decided() {
			toAccept.deps.Add(dependency.ID())
		}
	}

	dg.virtuousVoting.Remove(txNode.tx.ID())
	dg.pendingAccept.Register(toAccept)
}

func (dg *Directed) reject(ids ...ids.ID) error {
	for _, conflict := range ids {
		conflictKey := conflict.Key()
		conf := dg.txs[conflictKey]
		delete(dg.txs, conflictKey)

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

func (dg *Directed) redirectEdges(tx *directedTx) bool {
	changed := false
	for _, conflictID := range tx.outs.List() {
		changed = dg.redirectEdge(tx, conflictID) || changed
	}
	return changed
}

// Set the confidence of all conflicts to 0
// Change the direction of edges if needed
func (dg *Directed) redirectEdge(txNode *directedTx, conflictID ids.ID) bool {
	nodeID := txNode.tx.ID()
	conflict := dg.txs[conflictID.Key()]
	if txNode.bias <= conflict.bias {
		return false
	}

	// TODO: why is this confidence reset here? It should already be reset
	// implicitly by the lack of a timestamp increase.
	conflict.confidence = 0

	// Change the edge direction
	conflict.ins.Remove(nodeID)
	conflict.outs.Add(nodeID)
	dg.preferences.Remove(conflictID) // This consumer now has an out edge

	txNode.ins.Add(conflictID)
	txNode.outs.Remove(conflictID)
	if txNode.outs.Len() == 0 {
		// If I don't have out edges, I'm preferred
		dg.preferences.Add(nodeID)
	}
	return true
}

func (dg *Directed) removeConflict(id ids.ID, ids ...ids.ID) {
	for _, neighborID := range ids {
		neighborKey := neighborID.Key()
		// If the neighbor doesn't exist, they may have already been rejected
		if neighbor, exists := dg.txs[neighborKey]; exists {
			neighbor.ins.Remove(id)
			neighbor.outs.Remove(id)

			if neighbor.outs.Len() == 0 {
				// Make sure to mark the neighbor as preferred if needed
				dg.preferences.Add(neighborID)
			}

			dg.txs[neighborKey] = neighbor
		}
	}
}

type directedAccepter struct {
	dg       *Directed
	deps     ids.Set
	rejected bool
	txNode   *directedTx
}

func (a *directedAccepter) Dependencies() ids.Set { return a.deps }

func (a *directedAccepter) Fulfill(id ids.ID) {
	a.deps.Remove(id)
	a.Update()
}

func (a *directedAccepter) Abandon(id ids.ID) { a.rejected = true }

func (a *directedAccepter) Update() {
	// If I was rejected or I am still waiting on dependencies to finish do
	// nothing.
	if a.rejected || a.deps.Len() != 0 || a.dg.errs.Errored() {
		return
	}

	id := a.txNode.tx.ID()
	delete(a.dg.txs, id.Key())

	for _, inputID := range a.txNode.tx.InputIDs().List() {
		delete(a.dg.utxos, inputID.Key())
	}
	a.dg.virtuous.Remove(id)
	a.dg.preferences.Remove(id)

	// Reject the conflicts
	if err := a.dg.reject(a.txNode.ins.List()...); err != nil {
		a.dg.errs.Add(err)
		return
	}
	// Should normally be empty
	if err := a.dg.reject(a.txNode.outs.List()...); err != nil {
		a.dg.errs.Add(err)
		return
	}

	// Mark it as accepted
	if err := a.txNode.tx.Accept(); err != nil {
		a.dg.errs.Add(err)
		return
	}
	a.txNode.accepted = true
	a.dg.ctx.DecisionDispatcher.Accept(a.dg.ctx.ChainID, id, a.txNode.tx.Bytes())
	a.dg.metrics.Accepted(id)

	a.dg.pendingAccept.Fulfill(id)
	a.dg.pendingReject.Abandon(id)
}

// directedRejector implements Blockable
type directedRejector struct {
	dg       *Directed
	deps     ids.Set
	rejected bool // true if the transaction has been rejected
	txNode   *directedTx
}

func (r *directedRejector) Dependencies() ids.Set { return r.deps }

func (r *directedRejector) Fulfill(id ids.ID) {
	if r.rejected || r.dg.errs.Errored() {
		return
	}
	r.rejected = true
	r.dg.errs.Add(r.dg.reject(r.txNode.tx.ID()))
}

func (*directedRejector) Abandon(id ids.ID) {}

func (*directedRejector) Update() {}

type sortTxNodeData []*directedTx

func (tnd sortTxNodeData) Less(i, j int) bool {
	return bytes.Compare(
		tnd[i].tx.ID().Bytes(),
		tnd[j].tx.ID().Bytes()) == -1
}
func (tnd sortTxNodeData) Len() int      { return len(tnd) }
func (tnd sortTxNodeData) Swap(i, j int) { tnd[j], tnd[i] = tnd[i], tnd[j] }

func sortTxNodes(nodes []*directedTx) { sort.Sort(sortTxNodeData(nodes)) }
