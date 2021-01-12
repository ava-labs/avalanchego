package avalanche

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/events"
)

// Transition ID --> Epoch --> Dependent waiting on
// the transition to be issued in the epoch or accepted
// no later than the epoch
type txBlocker map[ids.ID]map[uint32][]events.Blockable

func (tb *txBlocker) init() {
	if *tb == nil {
		*tb = map[ids.ID]map[uint32][]events.Blockable{}
	}
}

// MarkIssued marks that transition [trID] was issued
// in [epoch]. A transition can be issued into the same
// epoch multiple times.
func (tb *txBlocker) markIssued(trID ids.ID, epoch uint32) {
	tb.init()

	epochToDependents, ok := (*tb)[trID]
	if !ok {
		// Nobody depended on this transition being issued
		return
	}
	dependents, ok := epochToDependents[epoch]
	if !ok {
		// Nobody depended on this transition being issued
		// in this epoch
		return
	}
	for _, dependent := range dependents {
		dependent.Fulfill(trID)
	}
	delete(epochToDependents, epoch)
	if len(epochToDependents) == 0 {
		// Nobody depends on this transition anymore
		delete(*tb, trID)
	} else {
		(*tb)[trID] = epochToDependents
	}
}

// MarkIssued marks that transition [trID] was accepted in [epoch].
// Assumes a transition is only accepted once.
func (tb *txBlocker) markAccepted(trID ids.ID, epoch uint32) {
	tb.init()

	epochToDependents, ok := (*tb)[trID]
	if !ok {
		// Nobody depended on this transition being issued
		return
	}

	// For every vertex that depends on this transition
	for depEpoch, dependents := range epochToDependents {
		if epoch <= depEpoch {
			// This vertex depended on [trID] being issued in [depEpoch]
			// or accepted in an epoch <= [depEpoch], which it has been.
			// Mark dependency as fulfilled.
			for _, dependent := range dependents {
				dependent.Fulfill(trID)
			}
		} else {
			// This transition won't be issued in [depEpoch]
			// or accepted in an epoch <= [depEpoch].
			for _, dependent := range dependents {
				dependent.Abandon(trID)
			}
		}
	}
	// Nobody is waiting on [trID] anymore.
	delete(*tb, trID)
}

// Mark that we don't have transition [trID] and don't
// expect to receive it
func (tb *txBlocker) abandon(trID ids.ID) {
	tb.init()

	epochToDependents := (*tb)[trID]
	for _, dependents := range epochToDependents {
		for _, dependent := range dependents {
			dependent.Abandon(trID)
		}
	}
	delete(*tb, trID)
}

// Register that [i] is waiting on all of its transition dependencies
// to be either issued in [epoch] or accepted in an epoch <= [epoch]
func (tb *txBlocker) register(i events.Blockable, epoch uint32) {
	tb.init()

	// For each dependency, mark that [i] depends on this dependency
	// being issued in [epoch] or accepted in an epoch <= [epoch]
	for dependency := range i.Dependencies() {
		epochToDependents, ok := (*tb)[dependency]
		if !ok {
			(*tb)[dependency] = map[uint32][]events.Blockable{epoch: {i}}
			continue
		}
		dependents := epochToDependents[epoch]
		dependents = append(dependents, i)
		epochToDependents[epoch] = dependents
	}
}
