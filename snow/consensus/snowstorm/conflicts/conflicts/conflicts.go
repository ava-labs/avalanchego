// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var (
	errInvalidTxType = errors.New("invalid tx type")
)

type Conflicts struct {
	// track the currently processing txs. Maps ID() to tx. This includes
	// transactions that have been added to the acceptable and rejectable
	// queues.
	txs map[ids.ID]Tx

	// transitions tracks the currently processing txIDs for a transition. Maps
	// transitionID to a set of txIDs.
	transitions map[ids.ID]ids.Set

	// track which txs are currently consuming which utxos. Maps inputID to a
	// set of txIDs.
	utxos map[ids.ID]ids.Set

	// track which txs are attempting to be restricted. Maps transitionID to a
	// set of txIDs.
	restrictions map[ids.ID]ids.Set

	// track which txs are blocking on others. Maps transitionID to a set of
	// txIDs.
	dependencies map[ids.ID]ids.Set

	// conditionallyAccepted tracks the set of txIDs that have been
	// conditionally accepted, but not returned as fully accepted.
	conditionallyAccepted ids.Set

	// accepted tracks the set of txIDs that have been added to the acceptable
	// queue.
	accepted ids.Set

	// track txs that have been marked as ready to accept
	acceptable []choices.Decidable

	// track txs that have been marked as ready to reject
	rejectable []choices.Decidable
}

func New() *Conflicts {
	return &Conflicts{
		txs:          make(map[ids.ID]Tx),
		transitions:  make(map[ids.ID]ids.Set),
		utxos:        make(map[ids.ID]ids.Set),
		restrictions: make(map[ids.ID]ids.Set),
		dependencies: make(map[ids.ID]ids.Set),
	}
}

// Add this tx to the conflict set. If this tx is of the correct type, this tx
// will be added to the set of processing txs. It is assumed this tx wasn't
// already processing. This will mark the consumed utxos and register a rejector
// that will be notified if a dependency of this tx was rejected.
func (c *Conflicts) Add(txIntf choices.Decidable) error {
	tx, ok := txIntf.(Tx)
	if !ok {
		return errInvalidTxType
	}

	txID := tx.ID()
	c.txs[txID] = tx

	transitionID := tx.TransitionID()
	txIDs := c.transitions[transitionID]
	txIDs.Add(txID)
	c.transitions[transitionID] = txIDs

	for inputKey := range tx.InputIDs() {
		spenders := c.utxos[inputKey]
		spenders.Add(txID)
		c.utxos[inputKey] = spenders
	}

	for _, restrictionID := range tx.Restrictions() {
		restrictors := c.restrictions[restrictionID]
		restrictors.Add(txID)
		c.restrictions[restrictionID] = restrictors
	}

	for _, dependencyID := range tx.Dependencies() {
		dependents := c.dependencies[dependencyID]
		dependents.Add(txID)
		c.dependencies[dependencyID] = dependents
	}
	return nil
}

// IsVirtuous checks the currently processing txs for conflicts. It is allowed
// to call this function with txs that aren't yet processing or currently
// processing txs.
func (c *Conflicts) IsVirtuous(txIntf choices.Decidable) (bool, error) {
	tx, ok := txIntf.(Tx)
	if !ok {
		return false, errInvalidTxType
	}

	txID := tx.ID()
	for inputKey := range tx.InputIDs() {
		spenders := c.utxos[inputKey]
		if numSpenders := spenders.Len(); numSpenders > 1 ||
			(numSpenders == 1 && !spenders.Contains(txID)) {
			return false, nil
		}
	}

	epoch := tx.Epoch()
	transitionID := tx.TransitionID()
	restrictors := c.restrictions[transitionID]
	for restrictorKey := range restrictors {
		restrictor := c.txs[restrictorKey]
		restrictorEpoch := restrictor.Epoch()
		if restrictorEpoch <= epoch {
			return false, nil
		}
	}
	return true, nil
}

// Conflicts returns the collection of txs that are currently processing that
// conflict with the provided tx. It is allowed to call with function with txs
// that aren't yet processing or currently processing txs.
func (c *Conflicts) Conflicts(txIntf choices.Decidable) ([]choices.Decidable, error) {
	tx, ok := txIntf.(Tx)
	if !ok {
		return nil, errInvalidTxType
	}

	txID := tx.ID()
	var conflictSet ids.Set
	conflictSet.Add(txID)

	conflicts := []choices.Decidable(nil)
	for inputID := range tx.InputIDs() {
		spenders := c.utxos[inputID]
		for spenderID := range spenders {
			if conflictSet.Contains(spenderID) {
				continue
			}
			conflictSet.Add(spenderID)
			conflicts = append(conflicts, c.txs[spenderID])
		}
	}

	epoch := tx.Epoch()
	transitionID := tx.TransitionID()
	restrictors := c.restrictions[transitionID]
	for restrictorID := range restrictors {
		if conflictSet.Contains(restrictorID) {
			continue
		}

		restrictor := c.txs[restrictorID]
		restrictorEpoch := restrictor.Epoch()
		if restrictorEpoch > epoch {
			continue
		}

		conflictSet.Add(restrictorID)
		conflicts = append(conflicts, restrictor)
	}
	return conflicts, nil
}

// Accept notifies this conflict manager that a tx has been conditionally
// accepted. This means that assuming all the txs this tx depends on are
// accepted, then this tx should be accepted as well. This
func (c *Conflicts) Accept(txID ids.ID) {
	tx, exists := c.txs[txID]
	if !exists {
		return
	}

	acceptable := true
	for _, dependency := range tx.Dependencies() {
		if !c.accepted.Contains(dependency) {
			acceptable = false
		}
	}
	if acceptable {
		transitionID := tx.TransitionID()
		c.accepted.Add(transitionID)
		c.acceptable = append(c.acceptable, tx)
	} else {
		c.conditionallyAccepted.Add(txID)
	}
}

func (c *Conflicts) Updateable() ([]choices.Decidable, []choices.Decidable) {
	accepted := c.accepted
	c.accepted = nil
	acceptable := c.acceptable
	c.acceptable = nil

	// rejected tracks the set of txIDs that have been rejected but not yet
	// removed from the graph.
	rejected := ids.Set{}

	for _, tx := range acceptable {
		tx := tx.(Tx)
		txID := tx.ID()
		transitionID := tx.TransitionID()
		epoch := tx.Epoch()

		// Remove the accepted transaction from the collection
		delete(c.txs, txID)

		// Remove the transitions map
		txIDs := c.transitions[transitionID]
		txIDs.Remove(txID)
		if txIDs.Len() == 0 {
			delete(c.transitions, transitionID)
		} else {
			c.transitions[transitionID] = txIDs
		}

		// Remove the UTXO mappings
		for inputID := range tx.InputIDs() {
			spenders := c.utxos[inputID]
			spenders.Remove(txID)
			if spenders.Len() == 0 {
				delete(c.utxos, inputID)
			} else {
				c.utxos[inputID] = spenders
			}
		}

		// Remove the restrictions
		for _, restrictionID := range tx.Restrictions() {
			restrictors := c.restrictions[restrictionID]
			restrictors.Remove(txID)
			if restrictors.Len() == 0 {
				delete(c.restrictions, restrictionID)
			} else {
				c.restrictions[restrictionID] = restrictors
			}
		}

		// Remove the dependency pointers of the transactions that depended on
		// this transition. If a transaction has been conditionally accepted and
		// it has no dependencies left, then it should be marked as accepted. If
		// Reject dependencies that are no longer valid to be accepted.
		dependents := c.dependencies[transitionID]
		delete(c.dependencies, transitionID)
	acceptedDependentLoop:
		for dependentID := range dependents {
			dependent := c.txs[dependentID]
			dependentEpoch := dependent.Epoch()
			if dependentEpoch < epoch && !rejected.Contains(dependentID) {
				rejected.Add(dependentID)
				c.rejectable = append(c.rejectable, dependent)
			}

			if !c.conditionallyAccepted.Contains(dependentID) {
				continue
			}

			for _, dependencyID := range dependent.Dependencies() {
				if !accepted.Contains(dependencyID) {
					continue acceptedDependentLoop
				}
			}

			dependentTransitionID := dependent.TransitionID()

			c.conditionallyAccepted.Remove(dependentID)
			c.accepted.Add(dependentTransitionID)
			c.acceptable = append(c.acceptable, dependent)
		}

		// Conflicts should never return an error, as the type has already been
		// asserted.
		conflicts, _ := c.Conflicts(tx)
		for _, conflict := range conflicts {
			conflictID := conflict.ID()
			if rejected.Contains(conflictID) {
				continue
			}
			rejected.Add(conflictID)
			c.rejectable = append(c.rejectable, conflict)
		}
	}

	rejectable := c.rejectable
	c.rejectable = nil
	for _, tx := range rejectable {
		tx := tx.(Tx)
		txID := tx.ID()
		transitionID := tx.TransitionID()

		// Remove the rejected transaction from the collection
		delete(c.txs, txID)

		// Remove the transitions map
		txIDs := c.transitions[transitionID]
		txIDs.Remove(txID)
		if txIDs.Len() == 0 {
			delete(c.transitions, transitionID)
		} else {
			c.transitions[transitionID] = txIDs
		}

		// Remove the UTXO mappings
		for inputID := range tx.InputIDs() {
			spenders := c.utxos[inputID]
			spenders.Remove(txID)
			if spenders.Len() == 0 {
				delete(c.utxos, inputID)
			} else {
				c.utxos[inputID] = spenders
			}
		}

		// Remove the restrictions
		for _, restrictionID := range tx.Restrictions() {
			restrictors := c.restrictions[restrictionID]
			restrictors.Remove(txID)
			if restrictors.Len() == 0 {
				delete(c.restrictions, restrictionID)
			} else {
				c.restrictions[restrictionID] = restrictors
			}
		}

		if txIDs.Len() == 0 {
			// Remove the dependency pointers of the transactions that depended
			// on this transition.
			dependents := c.dependencies[transitionID]
			for dependentID := range dependents {
				dependent := c.txs[dependentID]
				if !rejected.Contains(dependentID) {
					rejected.Add(dependentID)
					c.rejectable = append(c.rejectable, dependent)
				}
			}
			delete(c.dependencies, transitionID)
		} else {
			lowestRemainingEpoch := uint32(math.MaxUint32)
			for otherTxID := range txIDs {
				otherTx := c.txs[otherTxID]
				otherEpoch := otherTx.Epoch()
				if otherEpoch < lowestRemainingEpoch {
					lowestRemainingEpoch = otherEpoch
				}
			}

			// Remove the dependency pointers of the transactions that depended
			// on the transition to be performed in this epoch.
			dependents := c.dependencies[transitionID]
			for dependentID := range dependents {
				dependent := c.txs[dependentID]
				dependentEpoch := dependent.Epoch()
				if dependentEpoch < lowestRemainingEpoch && !rejected.Contains(dependentID) {
					rejected.Add(dependentID)
					c.rejectable = append(c.rejectable, dependent)
				}
			}
		}
	}

	return acceptable, rejectable
}
