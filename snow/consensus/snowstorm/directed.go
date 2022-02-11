// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/metrics"
	"github.com/ava-labs/avalanchego/snow/events"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

var _ Consensus = &Directed{}

// DirectedFactory implements Factory by returning a directed struct
type DirectedFactory struct{}

// New implements Factory
func (DirectedFactory) New() Consensus { return &Directed{} }

// Directed is an implementation of a multi-color, non-transitive, snowball
// instance
type Directed struct {
	// metrics that describe this consensus instance
	metrics.Latency
	metrics.Polls

	// context that this consensus instance is executing in
	ctx *snow.ConsensusContext

	// params describes how this instance was parameterized
	params sbcon.Parameters

	// each element of preferences is the ID of a transaction that is preferred
	preferences ids.Set

	// each element of virtuous is the ID of a transaction that is virtuous
	virtuous ids.Set

	// each element is in the virtuous set and is still being voted on
	virtuousVoting ids.Set

	// number of times RecordPoll has been called
	pollNumber uint64

	// keeps track of whether dependencies have been accepted
	pendingAccept events.Blocker

	// keeps track of whether dependencies have been rejected
	pendingReject events.Blocker

	// track any errors that occurred during callbacks
	errs wrappers.Errs

	// Key: Transaction ID
	// Value: Node that represents this transaction in the conflict graph
	txs map[ids.ID]*directedTx

	// Key: UTXO ID
	// Value: IDs of transactions that consume the UTXO specified in the key
	utxos map[ids.ID]ids.Set

	// map transaction ID to the set of whitelisted transaction IDs.
	whitelists map[ids.ID]ids.Set
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
	ctx *snow.ConsensusContext,
	params sbcon.Parameters,
) error {
	dg.ctx = ctx
	dg.params = params

	if err := dg.Latency.Initialize("txs", "transaction(s)", ctx.Log, "", ctx.Registerer); err != nil {
		return fmt.Errorf("failed to initialize latency metrics: %w", err)
	}
	if err := dg.Polls.Initialize("", ctx.Registerer); err != nil {
		return fmt.Errorf("failed to initialize poll metrics: %w", err)
	}

	dg.txs = make(map[ids.ID]*directedTx)
	dg.utxos = make(map[ids.ID]ids.Set)
	dg.whitelists = make(map[ids.ID]ids.Set)

	return params.Verify()
}

// Parameters implements the Snowstorm interface
func (dg *Directed) Parameters() sbcon.Parameters { return dg.params }

// Virtuous implements the ConflictGraph interface
func (dg *Directed) Virtuous() ids.Set { return dg.virtuous }

// Preferences implements the ConflictGraph interface
func (dg *Directed) Preferences() ids.Set { return dg.preferences }

func (dg *Directed) VirtuousVoting() ids.Set { return dg.virtuousVoting }

// Quiesce implements the ConflictGraph interface
func (dg *Directed) Quiesce() bool {
	numVirtuous := dg.virtuousVoting.Len()
	dg.ctx.Log.Verbo("Conflict graph has %d voting virtuous transactions",
		numVirtuous)
	return numVirtuous == 0
}

// Finalized implements the ConflictGraph interface
func (dg *Directed) Finalized() bool {
	numPreferences := dg.preferences.Len()
	dg.ctx.Log.Verbo("Conflict graph has %d preferred transactions",
		numPreferences)
	return numPreferences == 0
}

// HealthCheck returns information about the consensus health.
func (dg *Directed) HealthCheck() (interface{}, error) {
	numOutstandingTxs := dg.Latency.ProcessingLen()
	isOutstandingTxs := numOutstandingTxs <= dg.params.MaxOutstandingItems
	details := map[string]interface{}{
		"outstandingTransactions": numOutstandingTxs,
	}
	if !isOutstandingTxs {
		errorReason := fmt.Sprintf("number of outstanding txs %d > %d", numOutstandingTxs, dg.params.MaxOutstandingItems)
		return details, fmt.Errorf("snowstorm consensus is not healthy reason: %s", errorReason)
	}
	return details, nil
}

// shouldVote returns if the provided tx should be voted on to determine if it
// can be accepted. If the tx can be vacuously accepted, the tx will be accepted
// and will therefore not be valid to be voted on.
func (dg *Directed) shouldVote(tx Tx) (bool, error) {
	if dg.Issued(tx) {
		// If the tx was previously inserted, it shouldn't be re-inserted.
		return false, nil
	}

	txID := tx.ID()
	bytes := tx.Bytes()

	// Notify the IPC socket that this tx has been issued if the transaction has
	// a binary format.
	if len(bytes) > 0 {
		if err := dg.ctx.DecisionDispatcher.Issue(dg.ctx, txID, bytes); err != nil {
			return false, err
		}
	}

	// Notify the metrics that this transaction is being issued.
	dg.Latency.Issued(txID, dg.pollNumber)

	// If this tx has inputs, it needs to be voted on before being accepted.
	if inputs := tx.InputIDs(); len(inputs) != 0 {
		return true, nil
	}

	// Since this tx doesn't have any inputs, it's impossible for there to be
	// any conflicting transactions. Therefore, this transaction is treated as
	// vacuously accepted and doesn't need to be voted on.

	// Notify those listening for accepted txs if the transaction has
	// a binary format.
	if len(bytes) > 0 {
		// Note that DecisionDispatcher.Accept must be called before
		// tx.Accept to honor EventDispatcher.Accept's invariant.
		if err := dg.ctx.DecisionDispatcher.Accept(dg.ctx, txID, bytes); err != nil {
			return false, err
		}
	}

	if err := tx.Accept(); err != nil {
		return false, err
	}

	// Notify the metrics that this transaction was accepted.
	dg.Latency.Accepted(txID, dg.pollNumber)
	return false, nil
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
	if shouldVote, err := dg.shouldVote(tx); !shouldVote || err != nil {
		return err
	}

	txID := tx.ID()
	txNode := &directedTx{tx: tx}

	// First check the other whitelist transactions.
	for otherID, otherWhitelist := range dg.whitelists {
		// [txID] is not whitelisted by [otherWhitelist]
		if !otherWhitelist.Contains(txID) {
			otherNode := dg.txs[otherID]

			// The [otherNode] should be preferred over [txNode] because a newly
			// issued transaction's confidence is always 0 and times are broken
			// by first issued.
			dg.addEdge(txNode, otherNode)
		}
	}
	whitelist, isWhitelist, err := tx.Whitelist()
	if err != nil {
		return err
	}
	if isWhitelist {
		// Find all transactions that are not explicitly whitelisted and mark
		// them as conflicting.
		for otherID, otherNode := range dg.txs {
			// [otherID] is not whitelisted by [whitelist]
			if !whitelist.Contains(otherID) {
				// The [otherNode] should be preferred over [txNode] because a
				// newly issued transaction's confidence is always 0 and times
				// are broken by first issued.
				dg.addEdge(txNode, otherNode)
			}
		}

		// Record the whitelist for future calls.
		dg.whitelists[txID] = whitelist
	}

	// For each UTXO consumed by the tx:
	// * Add edges between this tx and txs that consume this UTXO
	// * Mark this tx as attempting to consume this UTXO
	for _, inputID := range tx.InputIDs() {
		// Get the set of txs that are currently processing that also consume
		// this UTXO
		spenders := dg.utxos[inputID]

		// Update txs conflicting with tx to account for its issuance
		for conflictIDKey := range spenders {
			// Get the node that contains this conflicting tx
			conflict := dg.txs[conflictIDKey]

			// Add all the txs that spend this UTXO to this txs conflicts. These
			// conflicting txs must be preferred over this tx. We know this because
			// this tx currently has a bias of 0 and the tie goes to the tx whose
			// bias was updated first.
			dg.addEdge(txNode, conflict)
		}

		// Add this tx to list of txs consuming the current UTXO
		spenders.Add(txID)

		// spenders may be nil initially, so we should re-map the set.
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
	return dg.registerRejector(tx)
}

// addEdge between the [src] and [dst] txs to represent a conflict.
//
// The edge goes from [src] to [dst]: [src] -> [dst].
//
// It is assumed that this is only called when [src] is being added. Which is
// why only [dst] is removed from the virtuous set and marked as rogue. [src]
// must be marked as rogue externally.
//
// For example:
// - TxA is issued
// - TxB is issued that consumes the same UTXO as TxA.
//   - [addEdge(TxB, TxA)] would be called to register the conflict.
func (dg *Directed) addEdge(src, dst *directedTx) {
	srcID, dstID := src.tx.ID(), dst.tx.ID()

	// Track the outbound edge from [src] to [dst].
	src.outs.Add(dstID)

	// Because we are adding a conflict, the transaction can't be virtuous.
	dg.virtuous.Remove(dstID)
	dg.virtuousVoting.Remove(dstID)
	dst.rogue = true

	// Track the inbound edge to [dst] from [src].
	dst.ins.Add(srcID)
}

func (dg *Directed) Remove(txID ids.ID) error {
	s := ids.Set{
		txID: struct{}{},
	}
	return dg.reject(s)
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
	// This is also used to track the number of polls required to accept/reject
	// a transaction.
	dg.pollNumber++

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

		txNode.RecordSuccessfulPoll(dg.pollNumber)

		// If the tx should be accepted, then we should defer its acceptance
		// until its dependencies are decided. If this tx was already marked to
		// be accepted, we shouldn't register it again.
		if !txNode.pendingAccept &&
			txNode.Finalized(dg.params.BetaVirtuous, dg.params.BetaRogue) {
			// Mark that this tx is pending acceptance so acceptance is only
			// registered once.
			txNode.pendingAccept = true

			if err := dg.registerAcceptor(txNode.tx); err != nil {
				return false, err
			}
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

	if len(dg.txs) > 0 {
		if metThreshold.Len() == 0 {
			dg.Failed()
		} else {
			dg.Successful()
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
			confidence:         txNode.Confidence(dg.pollNumber),
		})
	}
	return consensusString(nodes)
}

// accept the named txID and remove it from the graph
func (dg *Directed) accept(txID ids.ID) error {
	txNode := dg.txs[txID]
	// We are accepting the tx, so we should remove the node from the graph.
	delete(dg.txs, txID)
	delete(dg.whitelists, txID)

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
			delete(dg.whitelists, conflictKey)
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

		// It's statistically unlikely that something being rejected is
		// preferred. However, it's possible. Additionally, any transaction may
		// be removed at any time.
		delete(dg.preferences, conflictKey)
		delete(dg.virtuous, conflictKey)
		delete(dg.virtuousVoting, conflictKey)

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

// accept the provided tx.
func (dg *Directed) acceptTx(tx Tx) error {
	txID := tx.ID()
	dg.ctx.Log.Trace("accepting transaction %s", txID)

	// Notify those listening that this tx has been accepted if the transaction
	// has a binary format.
	if bytes := tx.Bytes(); len(bytes) > 0 {
		// Note that DecisionDispatcher.Accept must be called before
		// tx.Accept to honor EventDispatcher.Accept's invariant.
		if err := dg.ctx.DecisionDispatcher.Accept(dg.ctx, txID, bytes); err != nil {
			return err
		}
	}

	if err := tx.Accept(); err != nil {
		return err
	}

	// Update the metrics to account for this transaction's acceptance
	dg.Latency.Accepted(txID, dg.pollNumber)
	// If there is a tx that was accepted pending on this tx, the ancestor
	// should be notified that it doesn't need to block on this tx anymore.
	dg.pendingAccept.Fulfill(txID)
	// If there is a tx that was issued pending on this tx, the ancestor tx
	// doesn't need to be rejected because of this tx.
	dg.pendingReject.Abandon(txID)

	return nil
}

// reject the provided tx.
func (dg *Directed) rejectTx(tx Tx) error {
	txID := tx.ID()
	dg.ctx.Log.Trace("rejecting transaction %s due to a conflicting acceptance", txID)

	// Reject is called before notifying the IPC so that rejections that
	// cause fatal errors aren't sent to an IPC peer.
	if err := tx.Reject(); err != nil {
		return err
	}

	// Notify the IPC that the tx was rejected if the transaction has a binary
	// format.
	if bytes := tx.Bytes(); len(bytes) > 0 {
		if err := dg.ctx.DecisionDispatcher.Reject(dg.ctx, txID, bytes); err != nil {
			return err
		}
	}

	// Update the metrics to account for this transaction's rejection
	dg.Latency.Rejected(txID, dg.pollNumber)

	// If there is a tx that was accepted pending on this tx, the ancestor
	// tx can't be accepted.
	dg.pendingAccept.Abandon(txID)
	// If there is a tx that was issued pending on this tx, the ancestor tx
	// must be rejected.
	dg.pendingReject.Fulfill(txID)
	return nil
}

// registerAcceptor attempts to accept this tx once all its dependencies are
// accepted. If all the dependencies are already accepted, this function will
// immediately accept the tx.
func (dg *Directed) registerAcceptor(tx Tx) error {
	txID := tx.ID()

	toAccept := &acceptor{
		g:    dg,
		errs: &dg.errs,
		txID: txID,
	}

	deps, err := tx.Dependencies()
	if err != nil {
		return err
	}
	for _, dependency := range deps {
		if dependency.Status() != choices.Accepted {
			// If the dependency isn't accepted, then it must be processing.
			// This tx should be accepted after this tx is accepted. Note that
			// the dependencies can't already be rejected, because it is assumed
			// that this tx is currently considered valid.
			toAccept.deps.Add(dependency.ID())
		}
	}

	// This tx is no longer being voted on, so we remove it from the voting set.
	// This ensures that virtuous txs built on top of rogue txs don't force the
	// node to treat the rogue tx as virtuous.
	dg.virtuousVoting.Remove(txID)
	dg.pendingAccept.Register(toAccept)
	return nil
}

// registerRejector rejects this tx if any of its dependencies are rejected.
func (dg *Directed) registerRejector(tx Tx) error {
	// If a tx that this tx depends on is rejected, this tx should also be
	// rejected.
	toReject := &rejector{
		g:    dg,
		errs: &dg.errs,
		txID: tx.ID(),
	}

	// Register all of this txs dependencies as possibilities to reject this tx.
	deps, err := tx.Dependencies()
	if err != nil {
		return err
	}
	for _, dependency := range deps {
		if dependency.Status() != choices.Accepted {
			// If the dependency isn't accepted, then it must be processing. So,
			// this tx should be rejected if any of these processing txs are
			// rejected. Note that the dependencies can't already be rejected,
			// because it is assumed that this tx is currently considered valid.
			toReject.deps.Add(dependency.ID())
		}
	}

	// Register these dependencies
	dg.pendingReject.Register(toReject)
	return nil
}
