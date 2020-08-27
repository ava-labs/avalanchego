// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/formatting"

	sbcon "github.com/ava-labs/gecko/snow/consensus/snowball"
)

// InputFactory implements Factory by returning an input struct
type InputFactory struct{}

// New implements Factory
func (InputFactory) New() Consensus { return &Input{} }

// Input is an implementation of a multi-color, non-transitive, snowball
// instance
type Input struct {
	common

	// Key: Transaction ID
	// Value: Node that represents this transaction in the conflict graph
	txs map[[32]byte]*inputTx

	// Key: UTXO ID
	// Value: Node that represents the status of the transactions consuming this
	//        input
	utxos map[[32]byte]inputUTXO
}

type inputTx struct {
	// pendingAccept identifies if this transaction has been marked as accepted
	// once its transitive dependencies have also been accepted
	pendingAccept bool

	// numSuccessfulPolls is the number of times this tx was the successful
	// result of a network poll
	numSuccessfulPolls int

	// lastVote is the last poll number that this tx was included in a
	// successful network poll. This timestamp is needed to ensure correctness
	// in the case that a tx was rejected when it was preferred in a conflict
	// set and there was a tie for the second highest numSuccessfulPolls.
	lastVote int

	// tx is the actual transaction this node represents
	tx Tx
}

type inputUTXO struct {
	snowball

	// preference is the txID which snowball says this UTXO should prefer
	preference ids.ID

	// color is the txID which snowflake says this UTXO should prefer
	color ids.ID

	// spenders is the set of txIDs that are currently attempting to spend this
	// UTXO
	spenders ids.Set
}

// Initialize implements the ConflictGraph interface
func (ig *Input) Initialize(ctx *snow.Context, params sbcon.Parameters) error {
	ig.txs = make(map[[32]byte]*inputTx)
	ig.utxos = make(map[[32]byte]inputUTXO)

	return ig.common.Initialize(ctx, params)
}

// IsVirtuous implements the ConflictGraph interface
func (ig *Input) IsVirtuous(tx Tx) bool {
	txID := tx.ID()
	for _, utxoID := range tx.InputIDs().List() {
		utxo, exists := ig.utxos[utxoID.Key()]
		// If the UTXO wasn't currently processing, then this tx won't conflict
		// due to this UTXO.
		if !exists {
			continue
		}
		// If this UTXO is rogue, then this tx will have at least one conflict.
		if utxo.rogue {
			return false
		}
		// This UTXO is currently virtuous, so it must be spent by only one tx.
		// If that tx is different from this tx, then these txs would conflict.
		if !utxo.spenders.Contains(txID) {
			return false
		}
	}

	// None of the UTXOs consumed by this tx imply that this tx would be rogue,
	// so it is virtuous as far as this consensus instance knows.
	return true
}

// Conflicts implements the ConflictGraph interface
func (ig *Input) Conflicts(tx Tx) ids.Set {
	conflicts := ids.Set{}
	// The conflicting txs are the union of all the txs that spend an input that
	// this tx spends.
	for _, utxoID := range tx.InputIDs().List() {
		if utxo, exists := ig.utxos[utxoID.Key()]; exists {
			conflicts.Union(utxo.spenders)
		}
	}
	// A tx can't conflict with itself, so we should make sure to remove the
	// provided tx from the conflict set. This is needed in case this tx is
	// currently processing.
	conflicts.Remove(tx.ID())
	return conflicts
}

// Add implements the ConflictGraph interface
func (ig *Input) Add(tx Tx) error {
	if ig.Issued(tx) {
		// If the tx was previously inserted, it shouldn't be re-inserted.
		return nil
	}

	txID := tx.ID()
	bytes := tx.Bytes()

	// Notify the IPC socket that this tx has been issued.
	ig.ctx.DecisionDispatcher.Issue(ig.ctx.ChainID, txID, bytes)

	// Notify the metrics that this transaction was just issued.
	ig.metrics.Issued(txID)

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
		ig.ctx.DecisionDispatcher.Accept(ig.ctx.ChainID, txID, bytes)

		// Notify the metrics that this transaction was just accepted.
		ig.metrics.Accepted(txID)
		return nil
	}

	txNode := &inputTx{tx: tx}

	// This tx should be added to the virtuous sets and preferred sets if this
	// tx is virtuous in all of the UTXOs it is trying to consume.
	virtuous := true

	// For each UTXO consumed by the tx:
	// * Mark this tx as attempting to consume this UTXO
	// * Mark the UTXO as being rogue if applicable
	for _, inputID := range inputs.List() {
		inputKey := inputID.Key()
		utxo, exists := ig.utxos[inputKey]
		if exists {
			// If the utxo was already being consumed by another tx, this utxo
			// is now rogue.
			utxo.rogue = true
			// Since this utxo is rogue, this tx is rogue as well.
			virtuous = false
			// If this utxo was previously virtuous, then there may be txs that
			// were considered virtuous that are now known to be rogue. If
			// that's the case we should remove those txs from the virtuous
			// sets.
			for _, conflictID := range utxo.spenders.List() {
				ig.virtuous.Remove(conflictID)
				ig.virtuousVoting.Remove(conflictID)
			}
		} else {
			// If there isn't a conflict for this UTXO, I'm the preferred
			// spender.
			utxo.preference = txID
		}

		// This UTXO needs to track that it is being spent by this tx.
		utxo.spenders.Add(txID)

		// We need to write back
		ig.utxos[inputKey] = utxo
	}

	if virtuous {
		// If this tx is currently virtuous, add it to the virtuous sets
		ig.virtuous.Add(txID)
		ig.virtuousVoting.Add(txID)

		// If a tx is virtuous, it must be preferred.
		ig.preferences.Add(txID)
	}

	// Add this tx to the set of currently processing txs
	ig.txs[txID.Key()] = txNode

	// If a tx that this tx depends on is rejected, this tx should also be
	// rejected.
	toReject := &rejector{
		g:    ig,
		errs: &ig.errs,
		txID: txID,
	}

	// Register all of this txs dependencies as possibilities to reject this tx.
	for _, dependency := range tx.Dependencies() {
		if dependency.Status() != choices.Accepted {
			// If the dependency isn't accepted, then it must be processing. So,
			// this tx should be rejected if any of these processing txs are
			// rejected. Note that the dependencies can't already be rejected,
			// because it is assumped that this tx is currently considered
			// valid.
			toReject.deps.Add(dependency.ID())
		}
	}

	// Register these dependencies
	ig.pendingReject.Register(toReject)

	// Registering the rejector can't result in an error, so we can safely
	// return nil here.
	return nil
}

// Issued implements the ConflictGraph interface
func (ig *Input) Issued(tx Tx) bool {
	// If the tx is either Accepted or Rejected, then it must have been issued
	// previously.
	if tx.Status().Decided() {
		return true
	}

	// If the tx is currently processing, then it must have been issued.
	_, ok := ig.txs[tx.ID().Key()]
	return ok
}

// RecordPoll implements the ConflictGraph interface
func (ig *Input) RecordPoll(votes ids.Bag) (bool, error) {
	// Increase the vote ID. This is only updated here and is used to reset the
	// confidence values of transactions lazily.
	ig.currentVote++

	// This flag tracks if the Avalanche instance needs to recompute its
	// frontiers. Frontiers only need to be recalculated if preferences change
	// or if a tx was accepted.
	changed := false

	// We only want to iterate over txs that received alpha votes
	votes.SetThreshold(ig.params.Alpha)
	// Get the set of IDs that meet this alpha threshold
	metThreshold := votes.Threshold()
	for _, txID := range metThreshold.List() {
		txKey := txID.Key()

		// Get the node this tx represents
		txNode, exist := ig.txs[txKey]
		if !exist {
			// This tx may have already been accepted because of tx
			// dependencies. If this is the case, we can just drop the vote.
			continue
		}

		txNode.numSuccessfulPolls++
		txNode.lastVote = ig.currentVote

		// This tx is preferred if it is preferred in all of its conflict sets
		preferred := true
		// This tx is rogue if any of its conflict sets are rogue
		rogue := false
		// The confidence of the tx is the minimum confidence of all the input's
		// conflict sets
		confidence := math.MaxInt32
		for _, inputID := range txNode.tx.InputIDs().List() {
			inputKey := inputID.Key()
			utxo := ig.utxos[inputKey]

			// If this tx wasn't voted for during the last poll, the confidence
			// should have been reset during the last poll. So, we reset it now.
			// Additionally, if a different tx was voted for in the last poll,
			// the confidence should also be reset.
			if utxo.lastVote+1 != ig.currentVote || !txID.Equals(utxo.color) {
				utxo.confidence = 0
			}
			utxo.lastVote = ig.currentVote

			// Update the Snowflake counter and preference.
			utxo.color = txID
			utxo.confidence++

			// Update the Snowball preference.
			if txNode.numSuccessfulPolls > utxo.numSuccessfulPolls {
				// If this node didn't previous prefer this tx, then we need to
				// update the preferences.
				if !txID.Equals(utxo.preference) {
					// If the previous preference lost it's preference in this
					// input, it can't be preferred in all the inputs.
					if ig.preferences.Contains(utxo.preference) {
						ig.preferences.Remove(utxo.preference)
						// Because there was a change in preferences, Avalanche
						// will need to recompute its frontiers.
						changed = true
					}
					utxo.preference = txID
				}
				utxo.numSuccessfulPolls = txNode.numSuccessfulPolls
			} else {
				// This isn't the preferred choice in this conflict set so this
				// tx isn't be preferred.
				preferred = false
			}

			// If this utxo is rogue, the transaction must have at least one
			// conflict.
			rogue = rogue || utxo.rogue

			// The confidence of this tx is the minimum confidence of its
			// inputs.
			if confidence > utxo.confidence {
				confidence = utxo.confidence
			}

			// The input isn't a pointer, so it must be written back.
			ig.utxos[inputKey] = utxo
		}

		// If this tx is preferred and it isn't already marked as such, mark the
		// tx as preferred and for Avalanche to recompute the frontiers.
		if preferred && !ig.preferences.Contains(txID) {
			ig.preferences.Add(txID)
			changed = true
		}

		// If the tx should be accepted, then we should defer its acceptance
		// until its dependencies are decided. If this tx was already marked to
		// be accepted, we shouldn't register it again.
		if !txNode.pendingAccept &&
			((!rogue && confidence >= ig.params.BetaVirtuous) ||
				confidence >= ig.params.BetaRogue) {
			ig.deferAcceptance(txNode)
			if ig.errs.Errored() {
				return changed, ig.errs.Err
			}
		}

		if txNode.tx.Status() == choices.Accepted {
			// By accepting a tx, the state of this instance has changed.
			changed = true
		}
	}
	return changed, ig.errs.Err
}

func (ig *Input) String() string {
	nodes := make([]tempNode, 0, len(ig.txs))
	for _, tx := range ig.txs {
		id := tx.tx.ID()

		confidence := ig.params.BetaRogue
		for _, inputID := range tx.tx.InputIDs().List() {
			input := ig.utxos[inputID.Key()]
			if input.lastVote != ig.currentVote || !id.Equals(input.color) {
				confidence = 0
				break
			}
			if input.confidence < confidence {
				confidence = input.confidence
			}
		}

		nodes = append(nodes, tempNode{
			id:                 id,
			numSuccessfulPolls: tx.numSuccessfulPolls,
			confidence:         confidence,
		})
	}
	// Sort the nodes so that the string representation is canonical
	sortTempNodes(nodes)

	sb := strings.Builder{}
	sb.WriteString("IG(")

	format := fmt.Sprintf(
		"\n    Choice[%s] = ID: %%50s %%s",
		formatting.IntFormat(len(nodes)-1))
	for i, cn := range nodes {
		sb.WriteString(fmt.Sprintf(format, i, cn.id, &cn))
	}

	if len(nodes) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	return sb.String()
}

// deferAcceptance attempts to accept this tx once all its dependencies are
// accepted. If all the dependencies are already accepted, this function will
// immediately accept the tx.
func (ig *Input) deferAcceptance(txNode *inputTx) {
	// Mark that this tx is pending acceptance so this function won't be called
	// again
	txNode.pendingAccept = true

	toAccept := &inputAccepter{
		ig:     ig,
		txNode: txNode,
	}

	for _, dependency := range txNode.tx.Dependencies() {
		if dependency.Status() != choices.Accepted {
			// If the dependency isn't accepted, then it must be processing.
			// This tx should be accepted after this tx is accepted.
			toAccept.deps.Add(dependency.ID())
		}
	}

	// This tx is no longer being voted on, so we remove it from the voting set.
	// This ensures that virtuous txs built on top of rogue txs don't force the
	// node to treat the rogue tx as virtuous.
	ig.virtuousVoting.Remove(txNode.tx.ID())
	ig.pendingAccept.Register(toAccept)
}

// reject all the named txIDs and remove them from their conflict sets
func (ig *Input) reject(conflictIDs ...ids.ID) error {
	for _, conflictID := range conflictIDs {
		conflictKey := conflictID.Key()
		conflict := ig.txs[conflictKey]

		// We are rejecting the tx, so we should remove it from the graph
		delete(ig.txs, conflictKey)

		// While it's statistically unlikely that something being rejected is
		// preferred, it is handled for completion.
		ig.preferences.Remove(conflictID)

		// Remove this tx from all the conflict sets it's currently in
		ig.removeConflict(conflictID, conflict.tx.InputIDs().List()...)

		// Reject is called before notifying the IPC so that rejections that
		// cause fatal errors aren't sent to an IPC peer.
		if err := conflict.tx.Reject(); err != nil {
			return err
		}

		// Notify the IPC that the tx was rejected
		ig.ctx.DecisionDispatcher.Reject(ig.ctx.ChainID, conflict.tx.ID(), conflict.tx.Bytes())

		// Update the metrics to account for this transaction's rejection
		ig.metrics.Rejected(conflictID)

		// If there is a tx that was accepted pending on this tx, the ancestor
		// tx can't be accepted.
		ig.pendingAccept.Abandon(conflictID)
		// If there is a tx that was issued pending on this tx, the ancestor tx
		// must be rejected.
		ig.pendingReject.Fulfill(conflictID)
	}
	return nil
}

// Remove id from all of its conflict sets
func (ig *Input) removeConflict(txID ids.ID, inputIDs ...ids.ID) {
	for _, inputID := range inputIDs {
		inputKey := inputID.Key()
		utxo, exists := ig.utxos[inputKey]
		if !exists {
			// if the utxo doesn't exists, it was already consumed, so there is
			// no mapping left to update.
			continue
		}

		// This tx is no longer attempting to spend this utxo.
		utxo.spenders.Remove(txID)

		// If there is nothing attempting to consume the utxo anymore, remove it
		// from memory.
		if utxo.spenders.Len() == 0 {
			delete(ig.utxos, inputKey)
			continue
		}

		// If I'm rejecting the non-preference, there is nothing else to update.
		if !utxo.preference.Equals(txID) {
			ig.utxos[inputKey] = utxo
			continue
		}

		// If I was previously preferred, I must find who should now be
		// preferred.
		preference := ids.ID{}
		numSuccessfulPolls := -1
		lastVote := 0

		// Find the new Snowball preference
		for _, spender := range utxo.spenders.List() {
			txNode := ig.txs[spender.Key()]
			if txNode.numSuccessfulPolls > numSuccessfulPolls ||
				(txNode.numSuccessfulPolls == numSuccessfulPolls &&
					lastVote < txNode.lastVote) {
				preference = spender
				numSuccessfulPolls = txNode.numSuccessfulPolls
				lastVote = txNode.lastVote
			}
		}

		// Update the preferences
		utxo.preference = preference
		utxo.numSuccessfulPolls = numSuccessfulPolls

		ig.utxos[inputKey] = utxo

		// We need to check if this tx is now preferred
		txNode := ig.txs[preference.Key()]
		isPreferred := true
		for _, inputID := range txNode.tx.InputIDs().List() {
			inputKey := inputID.Key()
			input := ig.utxos[inputKey]

			if !preference.Equals(input.preference) {
				// If this preference isn't the preferred color, the tx isn't
				// preferred. Also note that the input might not exist, in which
				// case this tx is going to be rejected in a later iteration.
				isPreferred = false
				break
			}
		}
		if isPreferred {
			// If I'm preferred in all my conflict sets, I'm preferred.
			ig.preferences.Add(preference)
		}
	}
}

type inputAccepter struct {
	ig       *Input
	deps     ids.Set
	rejected bool
	txNode   *inputTx
}

func (a *inputAccepter) Dependencies() ids.Set { return a.deps }

func (a *inputAccepter) Fulfill(id ids.ID) {
	a.deps.Remove(id)
	a.Update()
}

func (a *inputAccepter) Abandon(id ids.ID) { a.rejected = true }

func (a *inputAccepter) Update() {
	// If I was rejected or I am still waiting on dependencies to finish or an
	// error has occurred, I shouldn't do anything.
	if a.rejected || a.deps.Len() != 0 || a.ig.errs.Errored() {
		return
	}

	txID := a.txNode.tx.ID()
	// We are accepting the tx, so we should remove the node from the graph.
	delete(a.ig.txs, txID.Key())

	// Get the conflicts of this tx so that we can reject them
	conflicts := a.ig.Conflicts(a.txNode.tx)

	// This tx is consuming all the UTXOs from its inputs, so we can prune them
	// all from memory
	for _, inputID := range a.txNode.tx.InputIDs().List() {
		delete(a.ig.utxos, inputID.Key())
	}

	// This tx is now accepted, so it shouldn't be part of the virtuous set or
	// the preferred set. Its status as Accepted implies these descriptions.
	a.ig.virtuous.Remove(txID)
	a.ig.preferences.Remove(txID)

	// Reject all the txs that conflicted with this tx.
	if err := a.ig.reject(conflicts.List()...); err != nil {
		a.ig.errs.Add(err)
		return
	}

	// Accept is called before notifying the IPC so that acceptances that cause
	// fatal errors aren't sent to an IPC peer.
	if err := a.txNode.tx.Accept(); err != nil {
		a.ig.errs.Add(err)
		return
	}

	// Notify the IPC socket that this tx has been accepted.
	a.ig.ctx.DecisionDispatcher.Accept(a.ig.ctx.ChainID, txID, a.txNode.tx.Bytes())

	// Update the metrics to account for this transaction's acceptance
	a.ig.metrics.Accepted(txID)

	// If there is a tx that was accepted pending on this tx, the ancestor
	// should be notified that it doesn't need to block on this tx anymore.
	a.ig.pendingAccept.Fulfill(txID)
	// If there is a tx that was issued pending on this tx, the ancestor tx
	// doesn't need to be rejected because of this tx.
	a.ig.pendingReject.Abandon(txID)
}

type tempNode struct {
	id                             ids.ID
	numSuccessfulPolls, confidence int
}

func (tn *tempNode) String() string {
	return fmt.Sprintf(
		"SB(NumSuccessfulPolls = %d, Confidence = %d)",
		tn.numSuccessfulPolls,
		tn.confidence)
}

type sortTempNodeData []tempNode

func (tnd sortTempNodeData) Less(i, j int) bool {
	return bytes.Compare(tnd[i].id.Bytes(), tnd[j].id.Bytes()) == -1
}
func (tnd sortTempNodeData) Len() int      { return len(tnd) }
func (tnd sortTempNodeData) Swap(i, j int) { tnd[j], tnd[i] = tnd[i], tnd[j] }

func sortTempNodes(nodes []tempNode) { sort.Sort(sortTempNodeData(nodes)) }
