// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"
)

// transitionNode contains data about a processing transition
type transitionNode struct {
	// [txIDs] is the set of processing txs that contain this transition.
	txIDs ids.Set

	// [restrictions] is the set of transaction IDs that require this
	// transition to be performed in that transaction's epoch or a
	// later one.
	restrictions ids.Set

	// [dependents] is the set of transactions that require this
	// transition to be performed before the transaction can be accepted.
	dependents ids.Set

	// [missingDependencies] is the set of transitions that must
	// be performed before this transition can be accepted.
	missingDependencies ids.Set
}

// Conflicts implements the [snowstorm.Conflicts] interface
type Conflicts struct {
	// tracks the currently processing txs. This includes
	// transactions in [acceptable] and [rejectable].
	// Transaction ID --> The transaction with that ID.
	txs map[ids.ID]Tx

	// tracks the currently processing transitions
	// Transition ID --> Data about that transition.
	transitionNodes map[ids.ID]transitionNode

	// tracks which txs consume which utxos.
	// UTXO ID --> IDs of transactions that consume the UTXO.
	utxos map[ids.ID]ids.Set

	// conditionallyAccepted tracks the set of txIDs that have been
	// conditionally accepted, but not returned as fully accepted.
	// A transaction is conditionally accepted if it has received enough
	// consecutive successful polls but has unmet dependencies.
	conditionallyAccepted ids.Set

	// acceptableIDs is the set of IDs of transitions that are in a
	// transaction in [acceptable].
	acceptableIDs ids.Set

	// acceptable is the set of txs that may be accepted.
	acceptable []Tx

	// rejectableIDs is the set of IDs of transactions in [rejectable].
	rejectableIDs ids.Set

	// rejectable is the set of txs that may be rejected.
	rejectable []Tx
}

// New returns a new Conflict Manager
func New() *Conflicts {
	return &Conflicts{
		txs:             make(map[ids.ID]Tx),
		utxos:           make(map[ids.ID]ids.Set),
		transitionNodes: make(map[ids.ID]transitionNode),
	}
}

// Add this tx to the conflict set. If this tx is of the correct type,
// this tx will be added to the set of processing txs.
// Invariant: Add has not already been called with argument [tx].
func (c *Conflicts) Add(tx Tx) error {
	// Mark that this tx is processing
	txID := tx.ID()
	c.txs[txID] = tx

	// Mark that this transaction contains this transition
	transition := tx.Transition()
	transitionID := transition.ID()

	transitionNode := c.transitionNodes[transitionID]
	transitionNode.txIDs.Add(txID)

	// Mark the UTXOs that this transition consumes
	for _, inputID := range transition.InputIDs() {
		spenders := c.utxos[inputID]
		spenders.Add(txID)
		c.utxos[inputID] = spenders
	}

	// Mark the transitions that this transaction depends
	// on being accepted in an epoch >= this transaction's epoch
	for _, restrictionID := range tx.Restrictions() {
		restrictionNode := c.transitionNodes[restrictionID]
		restrictionNode.restrictions.Add(txID)
		c.transitionNodes[restrictionID] = restrictionNode
	}

	// Mark this transition's dependencies
	missingDependencies := transition.Dependencies()
	for _, dependencyID := range missingDependencies {
		dependentNode := c.transitionNodes[dependencyID]
		dependentNode.dependents.Add(txID)
		c.transitionNodes[dependencyID] = dependentNode
	}

	transitionNode.missingDependencies.Add(missingDependencies...)
	c.transitionNodes[transitionID] = transitionNode

	return nil
}

// Processing returns true if transition [trID] is processing.
func (c *Conflicts) Processing(trID ids.ID) bool {
	_, exists := c.transitionNodes[trID]
	return exists
}

// IsVirtuous checks the currently processing txs for conflicts.
// [tx] need not be a tx that is processing.
func (c *Conflicts) IsVirtuous(tx Tx) (bool, error) {
	// Check whether this transaction consumes the same state as
	// a different processing transaction
	txID := tx.ID()
	transition := tx.Transition()
	for _, inputID := range transition.InputIDs() { // For each piece of state [tx] consumes
		spenders := c.utxos[inputID] // txs that consume this state
		if numSpenders := spenders.Len(); numSpenders > 1 ||
			(numSpenders == 1 && !spenders.Contains(txID)) {
			return false, nil // another tx consumes the same state
		}
	}

	// Check if there are any transactions that attempt to
	// restrict this transition to at least their epoch.
	epoch := tx.Epoch()
	transitionID := transition.ID()
	transitionNode := c.transitionNodes[transitionID]

	for restrictorID := range transitionNode.restrictions {
		restrictor := c.txs[restrictorID]
		restrictorEpoch := restrictor.Epoch()
		if restrictorEpoch > epoch {
			return false, nil
		}
	}

	// Check if this transaction is marked as attempting to restrict other txs
	// to at least this epoch.
	for _, restrictedTransitionID := range tx.Restrictions() {
		// Iterate over the set of transactions that [tx] requires
		// to be performed in [epoch] or a later one
		for restrictionID := range c.transitionNodes[restrictedTransitionID].txIDs {
			restriction := c.txs[restrictionID]
			restrictionEpoch := restriction.Epoch()
			if restrictionEpoch < epoch {
				return false, nil
			}
		}
	}
	return true, nil
}

// Conflicts returns the processing txs that conflict with [tx].
// [tx] need not be processing.
func (c *Conflicts) Conflicts(tx Tx) ([]Tx, error) {
	txID := tx.ID()
	var conflictSet ids.Set
	conflictSet.Add(txID)
	conflicts := []Tx(nil)
	transition := tx.Transition()

	// Add to the conflict set transactions that consume the same state as [tx]
	for _, inputID := range transition.InputIDs() {
		spenders := c.utxos[inputID]
		for spenderID := range spenders {
			if conflictSet.Contains(spenderID) { // already have this tx as a conflict
				continue
			}
			conflictSet.Add(spenderID)
			conflicts = append(conflicts, c.txs[spenderID])
		}
	}

	// Add to the conflict set transactions that require the transition performed
	// by [tx] to be in at least their epoch, but which are in an epoch after [tx]'s
	epoch := tx.Epoch()
	transitionID := transition.ID()
	for restrictorID := range c.transitionNodes[transitionID].restrictions {
		if conflictSet.Contains(restrictorID) {
			continue
		}
		restrictor := c.txs[restrictorID]
		restrictorEpoch := restrictor.Epoch()
		if restrictorEpoch > epoch {
			conflictSet.Add(restrictorID)
			conflicts = append(conflicts, restrictor)
		}
	}

	// Add to the conflict set transactions that perform a transition that [tx]
	// requires to occur in [epoch] or later, but which are in an earlier epoch.
	for _, restrictedTransitionID := range tx.Restrictions() {
		for restrictionID := range c.transitionNodes[restrictedTransitionID].txIDs {
			if conflictSet.Contains(restrictionID) {
				continue
			}
			restriction := c.txs[restrictionID]
			restrictionEpoch := restriction.Epoch()
			if restrictionEpoch < epoch {
				conflictSet.Add(restrictionID)
				conflicts = append(conflicts, restriction)
			}
		}
	}
	return conflicts, nil
}

// Accept notifies this conflict manager that tx [txID] has been conditionally
// accepted. That is, assuming all the txs this tx depends on are
// accepted, then this tx should be accepted as well.
func (c *Conflicts) Accept(txID ids.ID) {
	tx, exists := c.txs[txID]
	if !exists {
		return
	}

	transition := tx.Transition()
	transitionID := transition.ID()

	acceptable := true
	for dependency := range c.transitionNodes[transitionID].missingDependencies {
		if !c.acceptableIDs.Contains(dependency) {
			// [tx] is not acceptable because a transition it requires to have
			// been performed has not yet been
			acceptable = false
		}
	}
	if acceptable {
		c.acceptableIDs.Add(transitionID)
		c.acceptable = append(c.acceptable, tx)
	} else {
		c.conditionallyAccepted.Add(txID)
	}
}

// Updateable implements the [snowstorm.Conflicts] interface
func (c *Conflicts) Updateable() ([]Tx, []Tx) {
	acceptable := make([]Tx, 0, len(c.acceptable))
	rejectable := make([]Tx, 0)
	done := false
	for !done {
		acceptableInner, rejectableInner := c.updateable()
		acceptable = append(acceptable, acceptableInner...)
		rejectable = append(rejectable, rejectableInner...)
		done = len(acceptableInner)+len(rejectableInner) == 0
	}

	c.acceptableIDs.Clear()
	c.rejectableIDs.Clear()
	return acceptable, rejectable
}

// updateable implements the [snowstorm.Conflicts] interface
func (c *Conflicts) updateable() ([]Tx, []Tx) {
	acceptable := c.acceptable
	c.acceptable = nil

	// This loop does cleanup for the txs which are about to be accepted,
	// marks conditionally acceptable txs with all dependencies met as
	// acceptable, and marks conflicting txs as rejectable
	for _, tx := range acceptable {
		tx := tx.(Tx)
		txID := tx.ID()
		transition := tx.Transition()
		transitionID := transition.ID()
		epoch := tx.Epoch()

		// Remove the accepted transaction from the processing tx set
		delete(c.txs, txID)

		// Remove from the transitions map
		transitionNode := c.transitionNodes[transitionID]
		transitionNode.txIDs.Remove(txID)

		// Remove from the UTXO map
		for _, inputID := range transition.InputIDs() {
			spenders := c.utxos[inputID]
			spenders.Remove(txID)
			if spenders.Len() == 0 {
				delete(c.utxos, inputID)
			} else {
				c.utxos[inputID] = spenders
			}
		}

		// Remove from the restrictions map
		for _, restrictionID := range tx.Restrictions() {
			restrictorNode := c.transitionNodes[restrictionID]
			restrictorNode.restrictions.Remove(txID)
			c.transitionNodes[restrictionID] = restrictorNode
		}

		// Remove from the transition map
		delete(c.transitionNodes, transitionID)

		// Remove the dependency pointers of the transactions that depended on
		// [tx]'s transition. If a transaction has been conditionally accepted and
		// it has no dependencies left, then it should be marked as acceptable.
		// Reject dependencies that are no longer valid to be accepted.
	acceptedDependentLoop:
		// [transitionNode.dependents] is the set of transactions that require
		// [transition] to be performed before they are accepted
		for dependentTxID := range transitionNode.dependents {
			dependentTx := c.txs[dependentTxID]
			dependentEpoch := dependentTx.Epoch()

			dependentTransition := dependentTx.Transition()
			dependentTransitionID := dependentTransition.ID()

			// Mark that this depdendency has been fulfilled.
			dependentTransitionNode := c.transitionNodes[dependentTransitionID]
			dependentTransitionNode.missingDependencies.Remove(transitionID)
			c.transitionNodes[dependentTransitionID] = dependentTransitionNode

			if dependentEpoch < epoch && !c.rejectableIDs.Contains(dependentTxID) {
				// [dependentTx] requires [tx]'s transition to happen
				// no later than [epoch] but the transition happens after.
				// Therefore, [dependent] may no longer be accepted.
				c.rejectableIDs.Add(dependentTxID)
				c.rejectable = append(c.rejectable, dependentTx)
				continue
			}

			if !c.conditionallyAccepted.Contains(dependentTxID) {
				continue
			}

			// [dependent] has been conditionally accepted.
			// Check whether all of its transition dependencies are met
			for dependentTransitionID := range dependentTransitionNode.missingDependencies {
				if !c.acceptableIDs.Contains(dependentTransitionID) {
					continue acceptedDependentLoop
				}
			}

			// [dependentTx] has been conditionally accepted
			// and its transition dependencies have been met.
			// Therefore, it is acceptable.
			c.conditionallyAccepted.Remove(dependentTxID)
			c.acceptableIDs.Add(dependentTransitionID)
			c.acceptable = append(c.acceptable, dependentTx)
		}

		// Mark transactions that conflict with [tx] as rejectable.
		// Conflicts should never return an error, as the type has already been
		// asserted.
		conflictTxs, _ := c.Conflicts(tx)
		for _, conflictTx := range conflictTxs {
			if conflictTxID := conflictTx.ID(); !c.rejectableIDs.Contains(conflictTxID) {
				c.rejectableIDs.Add(conflictTxID)
				c.rejectable = append(c.rejectable, conflictTx)
			}
		}
	}

	rejectable := c.rejectable
	c.rejectable = nil
	// This loop does cleanup for the txs which are about to be rejected,
	// and marks txs as rejectable if they have a dependency/restriction that
	// can't be met.
	for _, tx := range rejectable {
		tx := tx.(Tx)
		txID := tx.ID()
		transition := tx.Transition()
		transitionID := transition.ID()
		transitionNode := c.transitionNodes[transitionID]

		// Remove [tx] from the processing tx set
		delete(c.txs, txID)

		// Remove [tx] from set of transactions that perform this transition
		transitionNode.txIDs.Remove(txID)
		c.transitionNodes[transitionID] = transitionNode

		// Remove [tx] from the UTXO map
		for _, inputID := range transition.InputIDs() {
			spenders := c.utxos[inputID]
			spenders.Remove(txID)
			if spenders.Len() == 0 {
				delete(c.utxos, inputID)
			} else {
				c.utxos[inputID] = spenders
			}
		}

		// Remove [tx] from the restrictions map
		for _, restrictionID := range tx.Restrictions() {
			restrictorNode := c.transitionNodes[restrictionID]
			restrictorNode.restrictions.Remove(txID)
			c.transitionNodes[restrictionID] = restrictorNode
		}

		if transitionNode.txIDs.Len() == 0 {
			// [tx] was the only processing tx that performs transition [transitionID].
			// Mark as rejectable transactions that depended on this transition.
			for dependentTxID := range transitionNode.dependents {
				if !c.rejectableIDs.Contains(dependentTxID) {
					c.rejectableIDs.Add(dependentTxID)
					dependentTx := c.txs[dependentTxID]
					c.rejectable = append(c.rejectable, dependentTx)
				}
			}

			// If the last transaction attempting to perform [transitionID]
			// has been rejected, then remove it from the transition map.
			delete(c.transitionNodes, transitionID)
		} else if len(transitionNode.dependents) > 0 {
			// There are processing tx's other than [tx] that
			// perform transition [transitionID].
			// Calculate the earliest epoch in which the transition may occur.
			lowestRemainingEpoch := uint32(math.MaxUint32)
			for otherTxID := range transitionNode.txIDs {
				otherTx := c.txs[otherTxID]
				otherEpoch := otherTx.Epoch()
				if otherEpoch < lowestRemainingEpoch {
					lowestRemainingEpoch = otherEpoch
				}
			}

			// Mark as rejectable txs that require transition [transitionID] to
			// occur earlier than it can.
			for dependentTxID := range transitionNode.dependents {
				dependentTx := c.txs[dependentTxID]
				dependentEpoch := dependentTx.Epoch()
				if dependentEpoch < lowestRemainingEpoch && !c.rejectableIDs.Contains(dependentTxID) {
					c.rejectableIDs.Add(dependentTxID)
					c.rejectable = append(c.rejectable, dependentTx)
				}
			}
		}

		// For each missing dependency of this transition, mark that [tx]
		// is no longer a dependent
		for dependency := range transitionNode.missingDependencies {
			dependentTransitionNode, exists := c.transitionNodes[dependency]
			if !exists {
				continue
			}
			dependentTransitionNode.dependents.Remove(txID)
			c.transitionNodes[dependency] = dependentTransitionNode
		}
	}

	return acceptable, rejectable
}
