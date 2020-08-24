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
	"github.com/ava-labs/gecko/snow/choices"
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
	// bias is the number of times this transaction was the successful result of
	// a network poll
	bias int

	// confidence is the number of consecutive times this transaction was the
	// successful result of a network poll as of [lastVote]
	confidence int

	// lastVote is the last poll number that this transaction was included in a
	// successful network poll
	lastVote int

	// rogue identifies if there is a known conflict with this transaction
	rogue bool

	// pendingAccept identifies if this transaction has been marked as accepted
	// once its transitive dependencies have also been accepted
	pendingAccept bool

	// accepted identifies if this transaction has been accepted. This should
	// only be set if [pendingAccept] is also set
	accepted bool

	// ins is the set of txIDs that this tx conflicts with that are less
	// preferred than this tx
	ins ids.Set

	// outs is the set of txIDs that this tx conflicts with that are more
	// preferred than this tx
	outs ids.Set

	// tx is the actual transaction this node represents
	tx Tx
}

// Initialize implements the Consensus interface
func (dg *Directed) Initialize(ctx *snow.Context, params snowball.Parameters) error {
	dg.txs = make(map[[32]byte]*directedTx)
	dg.utxos = make(map[[32]byte]ids.Set)

	return dg.common.Initialize(ctx, params)
}

// IsVirtuous implements the Consensus interface
func (dg *Directed) IsVirtuous(tx Tx) bool {
	txID := tx.ID()
	// If the tx is currently processing, we should just return if was registed
	// as rogue or not.
	if node, exists := dg.txs[txID.Key()]; exists {
		return !node.rogue
	}

	// The tx isn't processing, so we need to check to see if it conflicts with
	// any of the other txs that are currently processing.
	for _, input := range tx.InputIDs().List() {
		if _, exists := dg.utxos[input.Key()]; exists {
			// A currently processing tx names the same input as the provided
			// tx, so the provided tx would be rogue.
			return false
		}
	}

	// This tx is virtuous as far as this consensus instance knows.
	return true
}

// Conflicts implements the Consensus interface
func (dg *Directed) Conflicts(tx Tx) ids.Set {
	conflicts := ids.Set{}
	if node, exists := dg.txs[tx.ID().Key()]; exists {
		// If the tx is currently processing, the conflicting txs is just the
		// union of the inbound conflicts and the outbound conflicts.
		conflicts.Union(node.ins)
		conflicts.Union(node.outs)
	} else {
		// If the tx isn't currently processing, the conflicting txs is the
		// union of all the txs that spend an input that this tx spends.
		for _, input := range tx.InputIDs().List() {
			if spends, exists := dg.utxos[input.Key()]; exists {
				conflicts.Union(spends)
			}
		}
	}
	return conflicts
}

// Add implements the Consensus interface
func (dg *Directed) Add(tx Tx) error {
	if dg.Issued(tx) {
		// If the tx was previously inserted, nothing should be done here.
		return nil
	}

	txID := tx.ID()
	bytes := tx.Bytes()

	// Notify the IPC socket that this tx has been issued.
	dg.ctx.DecisionDispatcher.Issue(dg.ctx.ChainID, txID, bytes)

	// Notify the metrics that this transaction was just issued.
	dg.metrics.Issued(txID)

	inputs := tx.InputIDs()

	// If this tx doesn't have any inputs, it's impossible for there to be any
	// conflicting transactions. Therefore, this transaction is treated as
	// vacuously accepted.
	if inputs.Len() == 0 {
		// Accept is called before notifying the IPC so that acceptances that
		// cause fatal errors aren't sent to an IPC peer.
		if err := tx.Accept(); err != nil {
			return err
		}

		// Notify the IPC socket that this tx has been accepted.
		dg.ctx.DecisionDispatcher.Accept(dg.ctx.ChainID, txID, bytes)

		// Notify the metrics that this transaction was just accepted.
		dg.metrics.Accepted(txID)
		return nil
	}

	txNode := &directedTx{tx: tx}

	// For each UTXO consumed by the tx:
	// * Add edges between this tx and txs that consume this UTXO
	// * Mark this tx as attempting to consume this UTXO
	for _, inputID := range inputs.List() {
		inputKey := inputID.Key()

		// Get the set of txs that are currently processing that also consume
		// this UTXO
		spenders := dg.utxos[inputKey]

		// Add all the txs that spend this UTXO to this tx's conflicts that are
		// preferred over this tx. We know all these tx's are preferred over
		// this tx, because this tx currently has a bias of 0 and the tie-break
		// goes to the tx whose bias was updated first.
		txNode.outs.Union(spenders)

		// Update txs conflicting with tx to account for its issuance
		for _, conflictID := range spenders.List() {
			conflictKey := conflictID.Key()

			// Get the node that contains this conflicting tx
			conflict := dg.txs[conflictKey]

			// This conflicting tx can't be virtuous anymore. So we remove this
			// conflicting tx from any of the virtuous sets if it was previously
			// in them.
			dg.virtuous.Remove(conflictID)
			dg.virtuousVoting.Remove(conflictID)

			// This tx should be set to rogue if it wasn't rogue before.
			conflict.rogue = true

			// This conflicting tx is preferred over the tx being inserted, as
			// described above. So we add the conflict to the inbound set.
			conflict.ins.Add(txID)
		}

		// Add this tx to list of txs consuming the current UTXO
		spenders.Add(txID)

		// Because this isn't a pointer, we should re-map the set.
		dg.utxos[inputKey] = spenders
	}

	// Mark this transaction as rogue if had any conflicts registered above
	txNode.rogue = txNode.outs.Len() != 0

	if !txNode.rogue {
		// If this tx is currently virtuous, add it to the virtuous sets
		dg.virtuous.Add(txID)
		dg.virtuousVoting.Add(txID)

		// If a tx is virtuous, it must be preferred.
		dg.preferences.Add(txID)
	}

	// Add this tx to the set of currently processing txs
	dg.txs[txID.Key()] = txNode

	// This tx can be accepted only if all the txs it depends on are also
	// accepted. If any txs that this tx depends on are rejected, reject it.
	toReject := &directedRejector{
		dg:     dg,
		txNode: txNode,
	}

	// Register all of this txs dependencies as possibilities to reject this tx.
	for _, dependency := range tx.Dependencies() {
		if dependency.Status() != choices.Accepted {
			// If the dependency isn't accepted, then it must be processing. So,
			// this tx should be rejected if any of these processing txs are
			// rejected. Note that the dependencies can't be rejected, because
			// it is assumped that this tx is currently considered valid.
			toReject.deps.Add(dependency.ID())
		}
	}

	// Register these dependencies
	dg.pendingReject.Register(toReject)

	// Registering the rejector can't result in an error, so we can safely
	// return nil here.
	return nil
}

// Issued implements the Consensus interface
func (dg *Directed) Issued(tx Tx) bool {
	// If the tx is either Accepted or Rejected, then it must have been issued
	// previously.
	if tx.Status().Decided() {
		return true
	}

	// If the tx is currently processing, then it must have been issued.
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
