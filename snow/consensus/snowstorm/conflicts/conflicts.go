// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"
)

type transitionNode struct {
	// [txIDs] tracks the currently processing txIDs for a transition.
	txIDs ids.Set

	// [restrictions] is the set of transaction IDs that require the
	// transition to be performed in the transaction's epoch or a
	// later one if the transaction is to be accepted.
	restrictions ids.Set

	// [dependencies] is the set of transactions that require this
	// transition to be performed before the transactions can be
	// accepted.
	dependencies ids.Set

	// [missingDependencies] is the set of transitions that must
	// be performed before this transition can be accepted.
	missingDependencies ids.Set
}

// Conflicts implements the [snowstorm.Conflicts] interface
type Conflicts struct {
	// track the currently processing txs. This includes
	// transactions that have been added to the acceptable and rejectable
	// queues.
	// Transaction ID --> The transaction with that ID
	txs map[ids.ID]Tx

	// tracks the currently processing transitions
	transitionNodes map[ids.ID]transitionNode

	// track which txs are currently consuming which utxos.
	// UTXO ID --> IDs of transactions that consume the UTXO
	utxos map[ids.ID]ids.Set

	// conditionallyAccepted tracks the set of txIDs that have been
	// conditionally accepted, but not returned as fully accepted.
	conditionallyAccepted ids.Set

	// accepted tracks the set of txIDs that have been added to the acceptable
	// queue.
	accepted ids.Set

	// track txs that have been marked as ready to accept
	acceptable []Tx

	// track txs that have been marked as ready to reject
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

// Add this tx to the conflict set. If this tx is of the correct type, this tx
// will be added to the set of processing txs. It is assumed this tx wasn't
// already processing. This will mark the consumed utxos and register a rejector
// that will be notified if a dependency of this tx was rejected.
func (c *Conflicts) Add(tx Tx) error {
	txID := tx.ID()
	c.txs[txID] = tx

	transition := tx.Transition()
	transitionID := transition.ID()

	transitionNode := c.transitionNodes[transitionID]
	transitionNode.txIDs.Add(txID)

	for _, inputID := range transition.InputIDs() {
		spenders := c.utxos[inputID]
		spenders.Add(txID)
		c.utxos[inputID] = spenders
	}

	for _, restrictionID := range tx.Restrictions() {
		restrictionNode := c.transitionNodes[restrictionID]
		restrictionNode.restrictions.Add(txID)
		c.transitionNodes[restrictionID] = restrictionNode
	}

	missingDependencies := transition.Dependencies()
	for _, dependencyID := range missingDependencies {
		dependentNode := c.transitionNodes[dependencyID]
		dependentNode.dependencies.Add(txID)
		c.transitionNodes[dependencyID] = dependentNode
	}

	transitionNode.missingDependencies.Add(missingDependencies...)
	c.transitionNodes[transitionID] = transitionNode

	return nil
}

// Processing returns true if [trID] is being processed
func (c *Conflicts) Processing(trID ids.ID) bool {
	_, exists := c.transitionNodes[trID]
	return exists
}

// IsVirtuous checks the currently processing txs for conflicts. It is allowed
// to call this function with txs that aren't yet processing or currently
// processing txs.
func (c *Conflicts) IsVirtuous(tx Tx) (bool, error) {
	// Check whether this transaction consumes the same state as
	// a different processing transaction
	txID := tx.ID()
	transition := tx.Transition()
	for _, inputID := range transition.InputIDs() { // For each piece of state [tx] consumes
		spenders := c.utxos[inputID] // txs that consume this state
		if numSpenders := spenders.Len(); numSpenders > 1 ||
			(numSpenders == 1 && !spenders.Contains(txID)) {
			return false, nil
		}
	}

	// Check if other transactions have marked as attempting to restrict this tx
	// to at least their epoch.
	epoch := tx.Epoch()
	transitionID := transition.ID()
	transitionNode := c.transitionNodes[transitionID]

	// Check if there are any transactions that attempt to
	// restrict this transition to at least their epoch.
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

// Conflicts returns the collection of txs that are currently processing that
// conflict with the provided tx. It is allowed to call with function with txs
// that aren't yet processing or currently processing txs.
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
			if conflictSet.Contains(spenderID) {
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
	// requires to occur in [epoch] or later, but which are in an earlier epoch
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

// Accept notifies this conflict manager that a tx has been conditionally
// accepted. This means that, assuming all the txs this tx depends on are
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
		if !c.accepted.Contains(dependency) {
			// [tx] is not acceptable because a transition it requires to have
			// been performed has not yet been
			acceptable = false
		}
	}
	if acceptable {
		c.accepted.Add(transitionID)
		c.acceptable = append(c.acceptable, tx)
	} else {
		c.conditionallyAccepted.Add(txID)
	}
}

// Updateable implements the [snowstorm.Conflicts] interface
func (c *Conflicts) Updateable() ([]Tx, []Tx) {
	accepted := c.accepted
	c.accepted = nil
	acceptable := c.acceptable
	c.acceptable = nil

	// rejected tracks the set of txIDs that have been rejected but not yet
	// removed from the graph.
	rejected := ids.Set{}

	// Accept each acceptable tx
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

		// Remove the transition from the map now that it's
		// been accepted.
		delete(c.transitionNodes, transitionID)

		// Remove the dependency pointers of the transactions that depended on
		// [tx]'s transition. If a transaction has been conditionally accepted and
		// it has no dependencies left, then it should be marked as accepted.
		// Reject dependencies that are no longer valid to be accepted.
		// [transitionNode.dependencies] is the set of transactions that require
		// transition [transitionID] to be performed before they are accepted
	acceptedDependentLoop:
		for dependentTxID := range transitionNode.dependencies {
			dependentTx := c.txs[dependentTxID]
			dependentEpoch := dependentTx.Epoch()

			dependentTransition := dependentTx.Transition()
			dependentTransitionID := dependentTransition.ID()

			dependentTransitionNode := c.transitionNodes[dependentTransitionID]
			dependentTransitionNode.missingDependencies.Remove(transitionID)
			c.transitionNodes[dependentTransitionID] = dependentTransitionNode

			if dependentEpoch < epoch && !rejected.Contains(dependentTxID) {
				// [dependent] requires [tx]'s transition to happen
				// no later than [epoch] but the transition happens after.
				// Therefore, [dependent] may no longer be accepted.
				rejected.Add(dependentTxID)
				c.rejectable = append(c.rejectable, dependentTx)
				continue
			}

			if !c.conditionallyAccepted.Contains(dependentTxID) {
				continue
			}

			// [dependent] has been conditionally accepted.
			// Check whether all of its transition dependencies are met
			for dependentTransitionID := range dependentTransitionNode.missingDependencies {
				if !accepted.Contains(dependentTransitionID) {
					continue acceptedDependentLoop
				}
			}

			// [dependentTx] has been conditionally accepted
			// and its transition dependencies have been met.
			// Therefore, it is acceptable.
			c.conditionallyAccepted.Remove(dependentTxID)
			c.accepted.Add(dependentTransitionID)
			c.acceptable = append(c.acceptable, dependentTx)
		}

		// Once all of the dependencies have been updated, clear the
		// dependencies so that a rejected transaction attempting to
		// make the same transition will not cause this transition's
		// dependencies to be processed again (which can cause a
		// segfault if the tx has been removed from the txs map).
		transitionNode.dependencies.Clear()

		// Mark that the transactions that conflict with [tx] are rejectable
		// Conflicts should never return an error, as the type has already been
		// asserted.
		conflictTxs, _ := c.Conflicts(tx)
		for _, conflictTx := range conflictTxs {
			conflictTxID := conflictTx.ID()
			if rejected.Contains(conflictTxID) {
				continue
			}
			rejected.Add(conflictTxID)
			c.rejectable = append(c.rejectable, conflictTx)
		}
	}

	// Reject each rejectable transaction
	rejectable := c.rejectable
	c.rejectable = nil
	for _, tx := range rejectable {
		tx := tx.(Tx)
		txID := tx.ID()
		transition := tx.Transition()
		transitionID := transition.ID()

		// Remove the rejected transaction from the processing tx set
		delete(c.txs, txID)

		transitionNode := c.transitionNodes[transitionID]

		transitionNode.txIDs.Remove(txID)
		c.transitionNodes[transitionID] = transitionNode
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

		if transitionNode.txIDs.Len() == 0 {
			// [tx] was the only processing tx that performs transition [transitionID]
			// Remove the dependency pointers of the transactions that depended
			// on this transition.
			for dependentTxID := range transitionNode.dependencies {
				if !rejected.Contains(dependentTxID) {
					rejected.Add(dependentTxID)
					dependentTx := c.txs[dependentTxID]
					c.rejectable = append(c.rejectable, dependentTx)
				}
			}

			// If the last transaction attempting to perform [transitionID]
			// has been rejected, then remove it from the transition map.
			delete(c.transitionNodes, transitionID)
		} else if len(transitionNode.dependencies) > 0 {
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
			for dependentTxID := range transitionNode.dependencies {
				dependentTx := c.txs[dependentTxID]
				dependentEpoch := dependentTx.Epoch()
				if dependentEpoch < lowestRemainingEpoch {
					rejected.Add(dependentTxID)
					c.rejectable = append(c.rejectable, dependentTx)
				}
			}
		}

		// Remove this transaction from the dependency sets pointers of the
		// transitions that this transaction depended on.
		for dependency := range transitionNode.missingDependencies {
			dependentTransitionNode, exists := c.transitionNodes[dependency]
			if !exists {
				continue
			}
			dependentTransitionNode.dependencies.Remove(txID)
			c.transitionNodes[dependency] = dependentTransitionNode
		}
	}

	return acceptable, rejectable
}
