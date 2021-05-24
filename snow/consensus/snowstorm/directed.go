// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
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
	txs map[ids.ID]*directedTx

	// Key: UTXO ID
	// Value: IDs of transactions that consume the UTXO specified in the key
	utxos map[ids.ID]ids.Set
}

type directedTx struct {
	snowball

	// pendingAccept identifies if this transaction has been marked as accepted
	// once its transitive dependencies have also been accepted
	pendingAccept bool

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
func (dg *Directed) Initialize(
	ctx *snow.Context,
	params sbcon.Parameters,
) error {
	dg.txs = make(map[ids.ID]*directedTx)
	dg.utxos = make(map[ids.ID]ids.Set)

	return dg.common.Initialize(ctx, params)
}

// IsVirtuous implements the Consensus interface
func (dg *Directed) IsVirtuous(tx Tx) bool {
	txID := tx.ID()
	// If the tx is currently processing, we should just return if was
	// registered as rogue or not.
	if node, exists := dg.txs[txID]; exists {
		return !node.rogue
	}

	// The tx isn't processing, so we need to check to see if it conflicts with
	// any of the other txs that are currently processing.
	for _, utxoID := range tx.InputIDs() {
		if _, exists := dg.utxos[utxoID]; exists {
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
	var conflicts ids.Set
	if node, exists := dg.txs[tx.ID()]; exists {
		// If the tx is currently processing, the conflicting txs are just the
		// union of the inbound conflicts and the outbound conflicts.
		// Only bother to call Union, which will do a memory allocation, if ins or outs are non-empty.
		if node.ins.Len() > 0 || node.outs.Len() > 0 {
			conflicts.Union(node.ins)
			conflicts.Union(node.outs)
		}
	} else {
		// If the tx isn't currently processing, the conflicting txs are the
		// union of all the txs that spend an input that this tx spends.
		for _, inputID := range tx.InputIDs() {
			if spends, exists := dg.utxos[inputID]; exists {
				conflicts.Union(spends)
			}
		}
	}
	return conflicts
}

// Add implements the Consensus interface
func (dg *Directed) Add(tx Tx) error {
	if shouldVote, err := dg.shouldVote(dg, tx); !shouldVote || err != nil {
		return err
	}

	txID := tx.ID()
	txNode := &directedTx{tx: tx}

	// For each UTXO consumed by the tx:
	// * Add edges between this tx and txs that consume this UTXO
	// * Mark this tx as attempting to consume this UTXO
	for _, inputID := range tx.InputIDs() {
		// Get the set of txs that are currently processing that also consume
		// this UTXO
		spenders := dg.utxos[inputID]

		// Add all the txs that spend this UTXO to this txs conflicts. These
		// conflicting txs must be preferred over this tx. We know this because
		// this tx currently has a bias of 0 and the tie goes to the tx whose
		// bias was updated first.
		txNode.outs.Union(spenders)

		// Update txs conflicting with tx to account for its issuance
		for conflictIDKey := range spenders {
			// Get the node that contains this conflicting tx
			conflict := dg.txs[conflictIDKey]

			// This conflicting tx can't be virtuous anymore. So, we attempt to
			// remove it from all of the virtuous sets.
			delete(dg.virtuous, conflictIDKey)
			delete(dg.virtuousVoting, conflictIDKey)

			// This tx should be set to rogue if it wasn't rogue before.
			conflict.rogue = true

			// This conflicting tx is preferred over the tx being inserted, as
			// described above. So we add the conflict to the inbound set.
			conflict.ins.Add(txID)
		}

		// Add this tx to list of txs consuming the current UTXO
		spenders.Add(txID)

		// Because this isn't a pointer, we should re-map the set.
		dg.utxos[inputID] = spenders
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
	dg.txs[txID] = txNode

	// If a tx that this tx depends on is rejected, this tx should also be
	// rejected.
	dg.registerRejector(dg, tx)
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
	_, ok := dg.txs[tx.ID()]
	return ok
}

// RecordPoll implements the Consensus interface
func (dg *Directed) RecordPoll(votes ids.Bag) (bool, error) {
	// Increase the vote ID. This is only updated here and is used to reset the
	// confidence values of transactions lazily.
	dg.currentVote++

	// This flag tracks if the Avalanche instance needs to recompute its
	// frontiers. Frontiers only need to be recalculated if preferences change
	// or if a tx was accepted.
	changed := false

	// We only want to iterate over txs that received alpha votes
	votes.SetThreshold(dg.params.Alpha)
	// Get the set of IDs that meet this alpha threshold
	metThreshold := votes.Threshold()
	for txIDKey := range metThreshold {
		// Get the node this tx represents
		txNode, exist := dg.txs[txIDKey]
		if !exist {
			// This tx may have already been accepted because of tx
			// dependencies. If this is the case, we can just drop the vote.
			continue
		}

		txNode.RecordSuccessfulPoll(dg.currentVote)

		// If the tx should be accepted, then we should defer its acceptance
		// until its dependencies are decided. If this tx was already marked to
		// be accepted, we shouldn't register it again.
		if !txNode.pendingAccept &&
			txNode.Finalized(dg.params.BetaVirtuous, dg.params.BetaRogue) {
			// Mark that this tx is pending acceptance so acceptance is only
			// registered once.
			txNode.pendingAccept = true

			dg.registerAcceptor(dg, txNode.tx)
			if dg.errs.Errored() {
				return changed, dg.errs.Err
			}
		}

		if txNode.tx.Status() != choices.Accepted {
			// If this tx wasn't accepted, then this instance is only changed if
			// preferences changed.
			changed = dg.redirectEdges(txNode) || changed
		} else {
			// By accepting a tx, the state of this instance has changed.
			changed = true
		}
	}
	return changed, dg.errs.Err
}

func (dg *Directed) String() string {
	nodes := make([]*snowballNode, 0, len(dg.txs))
	for _, txNode := range dg.txs {
		nodes = append(nodes, &snowballNode{
			txID:               txNode.tx.ID(),
			numSuccessfulPolls: txNode.numSuccessfulPolls,
			confidence:         txNode.Confidence(dg.currentVote),
		})
	}
	return ConsensusString("DG", nodes)
}

// accept the named txID and remove it from the graph
func (dg *Directed) accept(txID ids.ID) error {
	txNode := dg.txs[txID]
	// We are accepting the tx, so we should remove the node from the graph.
	delete(dg.txs, txID)

	// This tx is consuming all the UTXOs from its inputs, so we can prune them
	// all from memory
	for _, inputID := range txNode.tx.InputIDs() {
		delete(dg.utxos, inputID)
	}

	// This tx is now accepted, so it shouldn't be part of the virtuous set or
	// the preferred set. Its status as Accepted implies these descriptions.
	dg.virtuous.Remove(txID)
	dg.preferences.Remove(txID)

	// Reject all the txs that conflicted with this tx.
	if err := dg.reject(txNode.ins); err != nil {
		return err
	}
	// While it is typically true that a tx this is being accepted is preferred,
	// it is possible for this to not be the case. So this is handled for
	// completeness.
	if err := dg.reject(txNode.outs); err != nil {
		return err
	}
	return dg.acceptTx(txNode.tx)
}

// reject all the named txIDs and remove them from the graph
func (dg *Directed) reject(conflictIDs ids.Set) error {
	for conflictKey := range conflictIDs {
		conflict := dg.txs[conflictKey]
		// This tx is no longer an option for consuming the UTXOs from its
		// inputs, so we should remove their reference to this tx.
		for _, inputID := range conflict.tx.InputIDs() {
			txIDs, exists := dg.utxos[inputID]
			if !exists {
				// This UTXO may no longer exist because it was removed due to
				// the acceptance of a tx. If that is the case, there is nothing
				// left to remove from memory.
				continue
			}
			delete(txIDs, conflictKey)
			if txIDs.Len() == 0 {
				// If this tx was the last tx consuming this UTXO, we should
				// prune the UTXO from memory entirely.
				delete(dg.utxos, inputID)
			} else {
				// If this UTXO still has txs consuming it, then we should make
				// sure this update is written back to the UTXOs map.
				dg.utxos[inputID] = txIDs
			}
		}

		// We are rejecting the tx, so we should remove it from the graph
		delete(dg.txs, conflictKey)

		// While it's statistically unlikely that something being rejected is
		// preferred, it is handled for completion.
		delete(dg.preferences, conflictKey)

		// remove the edge between this node and all its neighbors
		dg.removeConflict(conflictKey, conflict.ins)
		dg.removeConflict(conflictKey, conflict.outs)

		if err := dg.rejectTx(conflict.tx); err != nil {
			return err
		}
	}
	return nil
}

// redirectEdges attempts to turn outbound edges into inbound edges if the
// preferences have changed
func (dg *Directed) redirectEdges(tx *directedTx) bool {
	changed := false
	for conflictID := range tx.outs {
		changed = dg.redirectEdge(tx, conflictID) || changed
	}
	return changed
}

// Change the direction of this edge if needed. Returns true if the direction
// was switched.
// TODO replace
func (dg *Directed) redirectEdge(txNode *directedTx, conflictID ids.ID) bool {
	conflict := dg.txs[conflictID]
	if txNode.numSuccessfulPolls <= conflict.numSuccessfulPolls {
		return false
	}

	// Because this tx has a higher preference than the conflicting tx, we must
	// ensure that the edge is directed towards this tx.
	nodeID := txNode.tx.ID()

	// Change the edge direction according to the conflict tx
	conflict.ins.Remove(nodeID)
	conflict.outs.Add(nodeID)
	dg.preferences.Remove(conflictID) // This conflict has an outbound edge

	// Change the edge direction according to this tx
	txNode.ins.Add(conflictID)
	txNode.outs.Remove(conflictID)
	if txNode.outs.Len() == 0 {
		// If this tx doesn't have any outbound edges, it's preferred
		dg.preferences.Add(nodeID)
	}
	return true
}

func (dg *Directed) removeConflict(txIDKey ids.ID, neighborIDs ids.Set) {
	for neighborID := range neighborIDs {
		neighbor, exists := dg.txs[neighborID]
		if !exists {
			// If the neighbor doesn't exist, they may have already been
			// rejected, so this mapping can be skipped.
			continue
		}

		// Remove any edge to this tx.
		delete(neighbor.ins, txIDKey)
		delete(neighbor.outs, txIDKey)

		if neighbor.outs.Len() == 0 {
			// If this tx should now be preferred, make sure its status is
			// updated.
			dg.preferences.Add(neighborID)
		}
	}
}
