package avalanche

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/events"
)

// Epoch --> Transition ID --> Dependent waiting on
// the transition to be issued in the epoch or accepted
// no later than the epoch
type txBlocker map[uint32]map[ids.ID][]events.Blockable

func (tb *txBlocker) init() {
	if *tb == nil {
		*tb = map[uint32]map[ids.ID][]events.Blockable{}
	}
}

// MarkIssued marks that transition [trID] was issued
// in [epoch]. A transition can be issued into the same
// epoch multiple times.
func (tb *txBlocker) markIssued(trID ids.ID, epoch uint32) {
	tb.init()

	trToDependents, ok := (*tb)[epoch]
	if !ok {
		// Nobody depends on a transition in this epoch
		return
	}
	dependents, ok := trToDependents[trID]
	if !ok {
		// Nobody depends on this transition being issued
		// in this epoch or accepted <= this epoch
		return
	}
	for _, dependent := range dependents {
		dependent.Fulfill(trID)
	}
	delete(trToDependents, trID)
	if len(trToDependents) == 0 {
		// No dependencies on transitions in this epoch
		delete(*tb, epoch)
	} else {
		(*tb)[epoch] = trToDependents
	}
}

// MarkIssued marks that transition [trID] was accepted in [epoch].
// Assumes a transition is only accepted once.
func (tb *txBlocker) markAccepted(trID ids.ID, epoch uint32) {
	tb.init()

	for depEpoch, trToDependents := range *tb {
		// [dependents] are waiting on [trID] to be issued in
		// [depEpoch] or accepted in an epoch <= [depEpoch]
		dependents := trToDependents[trID]
		if epoch <= depEpoch {
			// [dependent] depended on [trID] being issued in [depEpoch]
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

		// Nobody is waiting on [trID] anymore.
		delete(trToDependents, trID)
		if len(trToDependents) == 0 {
			delete(*tb, depEpoch)
		}
	}
}

// [trID] hasn't been issued in [epoch] and we don't
// expect it to be
func (tb *txBlocker) abandon(trID ids.ID, epoch uint32) {
	tb.init()

	trToDependents, ok := (*tb)[epoch]
	if !ok { // no dependents to abandon
		return
	}
	dependents := trToDependents[trID]
	for _, dependent := range dependents {
		dependent.Abandon(trID)
	}
	delete(trToDependents, trID)
	if len(trToDependents) == 0 {
		delete(*tb, epoch)
	}
}

// Register that [b] is waiting on all of its transition dependencies
// to be either issued in [epoch] or accepted in an epoch <= [epoch].
// Assumes all the transitions [b] depends on are eventually
// issued or abandoned.
func (tb *txBlocker) register(b events.Blockable, epoch uint32) {
	tb.init()

	// For each dependency, mark that [i] depends on this dependency
	// being issued in [epoch] or accepted in an epoch <= [epoch]
	for dependency := range b.Dependencies() {
		trToDependents, ok := (*tb)[epoch]
		if !ok {
			(*tb)[epoch] = map[ids.ID][]events.Blockable{dependency: {b}}
			continue
		}
		trToDependents[dependency] = append(trToDependents[dependency], b)
	}
}
