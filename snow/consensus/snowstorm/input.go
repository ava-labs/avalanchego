// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
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
	txs map[ids.ID]*inputTx

	// Key: UTXO ID
	// Value: Node that represents the status of the transactions consuming this
	//        input
	utxos map[ids.ID]inputUTXO
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
	ig.txs = make(map[ids.ID]*inputTx)
	ig.utxos = make(map[ids.ID]inputUTXO)

	return ig.common.Initialize(ctx, params)
}

// IsVirtuous implements the ConflictGraph interface
func (ig *Input) IsVirtuous(tx Tx) bool {
	txID := tx.ID()
	for _, utxoID := range tx.InputIDs() {
		utxo, exists := ig.utxos[utxoID]
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
	var conflicts ids.Set
	// The conflicting txs are the union of all the txs that spend an input that
	// this tx spends.
	for _, utxoID := range tx.InputIDs() {
		if utxo, exists := ig.utxos[utxoID]; exists {
			conflicts.Union(utxo.spenders)
		}
	}
	// A tx can't conflict with itself, so we should make sure to remove the
	// provided tx from the conflict set. This is needed in case this tx is
	// currently processing.
	if conflicts != nil { // Don't bother removing tx.ID() if conflicts is empty
		conflicts.Remove(tx.ID())
	}
	return conflicts
}

// Add implements the ConflictGraph interface
func (ig *Input) Add(tx Tx) error {
	if shouldVote, err := ig.shouldVote(ig, tx); !shouldVote || err != nil {
		return err
	}

	txID := tx.ID()
	txNode := &inputTx{tx: tx}

	// This tx should be added to the virtuous sets and preferred sets if this
	// tx is virtuous in all of the UTXOs it is trying to consume.
	virtuous := true

	// For each UTXO consumed by the tx:
	// * Mark this tx as attempting to consume this UTXO
	// * Mark the UTXO as being rogue if applicable
	for _, inputID := range tx.InputIDs() {
		utxo, exists := ig.utxos[inputID]
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
			for conflictIDKey := range utxo.spenders {
				delete(ig.virtuous, conflictIDKey)
				delete(ig.virtuousVoting, conflictIDKey)
			}
		} else {
			// If there isn't a conflict for this UTXO, I'm the preferred
			// spender.
			utxo.preference = txID
		}

		// This UTXO needs to track that it is being spent by this tx.
		utxo.spenders.Add(txID)

		// We need to write back
		ig.utxos[inputID] = utxo
	}

	if virtuous {
		// If this tx is currently virtuous, add it to the virtuous sets
		ig.virtuous.Add(txID)
		ig.virtuousVoting.Add(txID)

		// If a tx is virtuous, it must be preferred.
		ig.preferences.Add(txID)
	}

	// Add this tx to the set of currently processing txs
	ig.txs[txID] = txNode

	// If a tx that this tx depends on is rejected, this tx should also be
	// rejected.
	ig.registerRejector(ig, tx)
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
	_, ok := ig.txs[tx.ID()]
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
	for txID := range metThreshold {
		// Get the node this tx represents
		txNode, exist := ig.txs[txID]
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
		for _, inputID := range txNode.tx.InputIDs() {
			utxo := ig.utxos[inputID]

			// If this tx wasn't voted for during the last poll, the confidence
			// should have been reset during the last poll. So, we reset it now.
			// Additionally, if a different tx was voted for in the last poll,
			// the confidence should also be reset.
			if utxo.lastVote+1 != ig.currentVote || txID != utxo.color {
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
				if txID != utxo.preference {
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
			ig.utxos[inputID] = utxo
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
			// Mark that this tx is pending acceptance so acceptance is only
			// registered once.
			txNode.pendingAccept = true

			ig.registerAcceptor(ig, txNode.tx)
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
	nodes := make([]*snowballNode, 0, len(ig.txs))
	for _, tx := range ig.txs {
		txID := tx.tx.ID()

		confidence := ig.params.BetaRogue
		for _, inputID := range tx.tx.InputIDs() {
			input := ig.utxos[inputID]
			if input.lastVote != ig.currentVote || txID != input.color {
				confidence = 0
				break
			}
			if input.confidence < confidence {
				confidence = input.confidence
			}
		}

		nodes = append(nodes, &snowballNode{
			txID:               txID,
			numSuccessfulPolls: tx.numSuccessfulPolls,
			confidence:         confidence,
		})
	}
	return ConsensusString("IG", nodes)
}

// accept the named txID and remove it from the graph
func (ig *Input) accept(txID ids.ID) error {
	txNode := ig.txs[txID]
	// We are accepting the tx, so we should remove the node from the graph.
	delete(ig.txs, txID)

	// Get the conflicts of this tx so that we can reject them
	conflicts := ig.Conflicts(txNode.tx)

	// This tx is consuming all the UTXOs from its inputs, so we can prune them
	// all from memory
	for _, inputID := range txNode.tx.InputIDs() {
		delete(ig.utxos, inputID)
	}

	// This tx is now accepted, so it shouldn't be part of the virtuous set or
	// the preferred set. Its status as Accepted implies these descriptions.
	ig.virtuous.Remove(txID)
	ig.preferences.Remove(txID)

	// Reject all the txs that conflicted with this tx.
	if err := ig.reject(conflicts); err != nil {
		return err
	}
	return ig.acceptTx(txNode.tx)
}

// reject all the named txIDs and remove them from their conflict sets
func (ig *Input) reject(conflictIDs ids.Set) error {
	for conflictKey := range conflictIDs {
		conflict := ig.txs[conflictKey]

		// We are rejecting the tx, so we should remove it from the graph
		delete(ig.txs, conflictKey)

		// While it's statistically unlikely that something being rejected is
		// preferred, it is handled for completion.
		delete(ig.preferences, conflictKey)

		// Remove this tx from all the conflict sets it's currently in
		ig.removeConflict(conflictKey, conflict.tx.InputIDs())

		if err := ig.rejectTx(conflict.tx); err != nil {
			return err
		}
	}
	return nil
}

// Remove id from all of its conflict sets
func (ig *Input) removeConflict(txID ids.ID, inputIDs []ids.ID) {
	for _, inputID := range inputIDs {
		utxo, exists := ig.utxos[inputID]
		if !exists {
			// if the utxo doesn't exists, it was already consumed, so there is
			// no mapping left to update.
			continue
		}

		// This tx is no longer attempting to spend this utxo.
		delete(utxo.spenders, txID)

		// If there is nothing attempting to consume the utxo anymore, remove it
		// from memory.
		if utxo.spenders.Len() == 0 {
			delete(ig.utxos, inputID)
			continue
		}

		// If I'm rejecting the non-preference, there is nothing else to update.
		if utxo.preference != txID {
			ig.utxos[inputID] = utxo
			continue
		}

		// If I was previously preferred, I must find who should now be
		// preferred.
		preference := ids.ID{}
		numSuccessfulPolls := -1
		lastVote := 0

		// Find the new Snowball preference
		for spender := range utxo.spenders {
			txNode := ig.txs[spender]
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

		ig.utxos[inputID] = utxo

		// We need to check if this tx is now preferred
		txNode := ig.txs[preference]
		isPreferred := true
		for _, inputID := range txNode.tx.InputIDs() {
			input := ig.utxos[inputID]

			if preference != input.preference {
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
