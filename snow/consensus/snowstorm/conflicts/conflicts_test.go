// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

func TestProcessing(t *testing.T) {
	c := New()

	tx := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
	}

	processing := c.Processing(tx.Transition().ID())
	assert.False(t, processing)

	err := c.Add(tx)
	assert.NoError(t, err)

	processing = c.Processing(tx.Transition().ID())
	assert.True(t, processing)
}

func TestNoConflicts(t *testing.T) {
	c := New()

	tx := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
	}

	virtuous, err := c.IsVirtuous(tx)
	assert.NoError(t, err)
	assert.True(t, virtuous)

	conflicts := c.Conflicts(tx)
	assert.Empty(t, conflicts)
}

func TestInputConflicts(t *testing.T) {
	c := New()

	inputIDs := []ids.ID{ids.GenerateTestID()}
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:       ids.GenerateTestID(),
			InputIDsV: inputIDs,
			StatusV:   choices.Processing,
		},
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:       ids.GenerateTestID(),
			InputIDsV: inputIDs,
			StatusV:   choices.Processing,
		},
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	virtuous, err := c.IsVirtuous(tx1)
	assert.NoError(t, err)
	assert.False(t, virtuous)

	conflicts := c.Conflicts(tx1)
	assert.Len(t, conflicts, 1)
}

func TestOuterRestrictionConflicts(t *testing.T) {
	c := New()

	transitionID := ids.GenerateTestID()
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:     transitionID,
			StatusV: choices.Processing,
		},
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		EpochV:        1,
		RestrictionsV: []ids.ID{transitionID},
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	virtuous, err := c.IsVirtuous(tx1)
	assert.NoError(t, err)
	assert.False(t, virtuous)

	conflicts := c.Conflicts(tx1)
	assert.Len(t, conflicts, 1)
}

func TestInnerRestrictionConflicts(t *testing.T) {
	c := New()

	transitionID := ids.GenerateTestID()
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:     transitionID,
			StatusV: choices.Processing,
		},
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		EpochV:        1,
		RestrictionsV: []ids.ID{transitionID},
	}

	err := c.Add(tx1)
	assert.NoError(t, err)

	virtuous, err := c.IsVirtuous(tx0)
	assert.NoError(t, err)
	assert.False(t, virtuous)

	conflicts := c.Conflicts(tx0)
	assert.Len(t, conflicts, 1)
}

func TestAcceptNoConflicts(t *testing.T) {
	c := New()

	tx := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
	}

	err := c.Add(tx)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)

	toAccept := toAccepts[0]
	assert.Equal(t, tx.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)
}

func TestAcceptNoConflictsWithDependency(t *testing.T) {
	c := New()

	transitionID := ids.GenerateTestID()
	tr0 := &TestTransition{
		IDV:     transitionID,
		StatusV: choices.Processing,
	}
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr0,
	}
	tr1 := &TestTransition{
		IDV:           ids.GenerateTestID(),
		DependenciesV: []Transition{tr0},
		StatusV:       choices.Processing,
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr1,
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	err = c.Add(tx1)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx0.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept := toAccepts[0]
	assert.Equal(t, tx0.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)

	toAccept = toAccepts[0]
	assert.Equal(t, tx1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)
}

func TestNoConflictsNoEarlyAcceptDependency(t *testing.T) {
	c := New()

	tr0 := &TestTransition{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr0,
	}
	tr1 := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		DependenciesV: []Transition{tr0},
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr1,
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	err = c.Add(tx1)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx0.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept := toAccepts[0]
	assert.Equal(t, tx0.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)

	toAccept = toAccepts[0]
	assert.Equal(t, tx1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)
}

func TestAcceptNoConflictsWithDependenciesAcrossMultipleRounds(t *testing.T) {
	c := New()

	tr0 := &TestTransition{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr0,
	}
	tr1 := &TestTransition{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr1,
	}
	tr2 := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		DependenciesV: []Transition{tr0, tr1},
	}
	tx2 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr2,
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	err = c.Add(tx1)
	assert.NoError(t, err)

	err = c.Add(tx2)
	assert.NoError(t, err)

	// Check that no transactions are mistakenly marked
	// as accepted/rejected

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	// Accept tx2 and ensure that it is marked
	// as conditionally accepted pending its
	// dependencies.
	c.Accept(tx2.ID())

	assert.Equal(t, c.conditionallyAccepted.Len(), 1)

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)
	assert.Equal(t, c.conditionallyAccepted.Len(), 1)

	// Accept tx1 and ensure that it is the only
	// transaction marked as accepted. Note: tx2
	// still requires tx0 to be accepted.
	c.Accept(tx1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept := toAccepts[0]
	assert.Equal(t, tx1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	// Ensure that additional call to updateable
	// does not return any new accepted/rejected txs.

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 0)
	assert.Len(t, toRejects, 0)

	// Accept tx0 and ensure that it is
	// returned from Updateable
	c.Accept(tx0.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept = toAccepts[0]
	assert.Equal(t, tx0.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	// tx2 should be returned by the subseqeuent call

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept = toAccepts[0]
	assert.Equal(t, tx2.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)
}

func TestAcceptRejectedDependency(t *testing.T) {
	c := New()

	inputIDs := []ids.ID{ids.GenerateTestID()}
	tr0 := &TestTransition{
		IDV:       ids.GenerateTestID(),
		InputIDsV: inputIDs,
		StatusV:   choices.Processing,
	}
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr0,
	}
	tr1 := &TestTransition{
		IDV:       ids.GenerateTestID(),
		InputIDsV: inputIDs,
		StatusV:   choices.Processing,
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr1,
	}
	tx2 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:           ids.GenerateTestID(),
			DependenciesV: []Transition{tr0},
			StatusV:       choices.Processing,
		},
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	err = c.Add(tx1)
	assert.NoError(t, err)

	err = c.Add(tx2)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 1)

	toAccept := toAccepts[0]
	assert.Equal(t, tx1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	toReject := toRejects[0]
	assert.Equal(t, tx0.ID(), toReject.ID())
	err = toReject.Reject()
	assert.NoError(t, err)

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Len(t, toRejects, 1)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)

	toReject = toRejects[0]
	assert.Equal(t, tx2.ID(), toReject.ID())
	err = toReject.Reject()
	assert.NoError(t, err)
}

func TestAcceptRejectedEpochDependency(t *testing.T) {
	c := New()

	inputIDs := []ids.ID{ids.GenerateTestID()}
	tr := &TestTransition{
		IDV:       ids.GenerateTestID(),
		InputIDsV: inputIDs,
		StatusV:   choices.Processing,
	}
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr,
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr,
		EpochV:      1,
	}
	tx2 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:           ids.GenerateTestID(),
			DependenciesV: []Transition{tr},
			StatusV:       choices.Processing,
		},
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	err = c.Add(tx1)
	assert.NoError(t, err)

	err = c.Add(tx2)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 2)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)

	toAccept := toAccepts[0]
	assert.Equal(t, tx1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	for i, toReject := range toRejects {
		err := toReject.Reject()
		assert.NoError(t, err, "Error rejecting the %d rejectable transaction %s", i, toReject.ID())
	}
}

func TestAcceptRestrictedDependency(t *testing.T) {
	c := New()

	inputIDs := []ids.ID{ids.GenerateTestID()}
	trA := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: inputIDs,
	}
	trB := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID()},
		DependenciesV: []Transition{trA},
	}
	trC := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{ids.GenerateTestID()},
	}

	txA0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
	}
	txA1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
		EpochV:      1,
	}
	txB0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trB,
	}
	txB1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trB,
		EpochV:      1,
	}
	txC0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV:   trC,
		RestrictionsV: []ids.ID{trA.ID()},
	}
	txC1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV:   trC,
		EpochV:        1,
		RestrictionsV: []ids.ID{trA.ID()},
	}

	err := c.Add(txA0)
	assert.NoError(t, err)

	err = c.Add(txA1)
	assert.NoError(t, err)

	err = c.Add(txB0)
	assert.NoError(t, err)

	err = c.Add(txB1)
	assert.NoError(t, err)

	err = c.Add(txC0)
	assert.NoError(t, err)

	err = c.Add(txC1)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	// Accepting tx1 should restrict trA to epoch 1 rejecting
	// txA0 and txB0 as a result.
	c.Accept(txC1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 2)

	toAccept := toAccepts[0]
	assert.Equal(t, txC1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	for i, toReject := range toRejects {
		err = toReject.Reject()
		assert.NoError(t, err, "Error rejecting the %d rejectable transaction %s", i, toReject.ID())
	}

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Len(t, toRejects, 1)

	toReject := toRejects[0]
	assert.Equal(t, txB0.ID(), toReject.ID())
	err = toReject.Reject()
	assert.NoError(t, err)

	c.Accept(txA1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept = toAccepts[0]
	assert.Equal(t, txA1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	c.Accept(txB1.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept = toAccepts[0]
	assert.Equal(t, txB1.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)
}

func TestRejectedRejectedDependency(t *testing.T) {
	c := New()

	inputIDA := ids.GenerateTestID()
	inputIDB := ids.GenerateTestID()

	//   A.X - A.Y
	//          |
	//   B.X - B.Y
	trAX := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{inputIDA, ids.GenerateTestID()},
	}
	txAX := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trAX,
	}
	trAY := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{inputIDA},
	}
	txAY := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trAY,
	}
	trBX := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{inputIDB},
	}
	txBX := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trBX,
	}
	trBY := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		DependenciesV: []Transition{trAY},
		InputIDsV:     []ids.ID{inputIDB},
	}
	txBY := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trBY,
	}

	err := c.Add(txAY)
	assert.NoError(t, err)

	err = c.Add(txAX)
	assert.NoError(t, err)

	err = c.Add(txBY)
	assert.NoError(t, err)

	err = c.Add(txBX)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(txBX.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 1)

	toAccept := toAccepts[0]
	assert.Equal(t, txBX.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	toReject := toRejects[0]
	// assert.Equal(t, ) TODO
	err = toReject.Reject()
	assert.NoError(t, err)

	c.Accept(txAY.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 1)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)

	toAccept = toAccepts[0]
	assert.Equal(t, txAY.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	toReject = toRejects[0]
	// check equal to what TODO
	err = toReject.Reject()
	assert.NoError(t, err)
}

func TestAcceptVirtuousRejectedDependency(t *testing.T) {
	c := New()

	inputIDsA := []ids.ID{ids.GenerateTestID()}
	inputIDsB := []ids.ID{ids.GenerateTestID()}

	//   A.X - A.Y
	//          |
	//   B.X - B.Y
	trAX := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: inputIDsA,
	}
	txAX := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trAX,
	}
	trAY := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: inputIDsA,
	}
	txAY := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trAY,
	}
	trBX := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: inputIDsB,
	}
	txBX := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trBX,
	}
	trV := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		DependenciesV: []Transition{trAY},
		InputIDsV:     inputIDsB,
	}
	txBY := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trV,
	}

	err := c.Add(txAX)
	assert.NoError(t, err)

	err = c.Add(txAY)
	assert.NoError(t, err)

	err = c.Add(txBX)
	assert.NoError(t, err)

	err = c.Add(txBY)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(txAX.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 1)

	toAccept := toAccepts[0]
	assert.Equal(t, txAX.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	toReject := toRejects[0]
	assert.Equal(t, txAY.ID(), toReject.ID())
	err = toReject.Reject()
	assert.NoError(t, err)

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 0)
	assert.Len(t, toRejects, 1)

	toReject = toRejects[0]
	assert.Equal(t, txBY.ID(), toReject.ID())
	err = toReject.Reject()
	assert.NoError(t, err)

	c.Accept(txBX.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 0)

	toAccept = toAccepts[0]
	assert.Equal(t, txBX.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)
}

func TestRejectDependencyTwice(t *testing.T) {
	c := New()

	trA := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{ids.GenerateTestID()},
	}
	trB := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID()},
		DependenciesV: []Transition{trA},
	}
	trC := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID()},
		DependenciesV: []Transition{trB},
	}

	txA0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
	}
	txA1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
		EpochV:      1,
	}
	txA2 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
		EpochV:      2,
	}
	txB0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trB,
	}
	txB1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trB,
		EpochV:      1,
	}
	txB2 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trB,
		EpochV:      2,
	}
	txC0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trC,
	}
	txC1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trC,
		EpochV:      1,
	}
	txC2 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trC,
		EpochV:      2,
	}

	err := c.Add(txA0)
	assert.NoError(t, err)

	err = c.Add(txA1)
	assert.NoError(t, err)

	err = c.Add(txA2)
	assert.NoError(t, err)

	err = c.Add(txB0)
	assert.NoError(t, err)

	err = c.Add(txB1)
	assert.NoError(t, err)

	err = c.Add(txB2)
	assert.NoError(t, err)

	err = c.Add(txC0)
	assert.NoError(t, err)

	err = c.Add(txC1)
	assert.NoError(t, err)

	err = c.Add(txC2)
	assert.NoError(t, err)

	c.Accept(txA2.ID())

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 4)

	toAccept := toAccepts[0]
	assert.Equal(t, txA2.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	for i, toReject := range toRejects {
		err = toReject.Reject()
		assert.NoError(t, err, "Error rejecting the %d rejectable transaction %s", i, toReject.ID())
	}

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Len(t, toRejects, 2)

	for i, toReject := range toRejects {
		err = toReject.Reject()
		assert.NoError(t, err, "Error rejecting the %d rejectable transaction %s", i, toReject.ID())
	}

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(txB2.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept = toAccepts[0]
	assert.Equal(t, txB2.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	c.Accept(txC2.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Empty(t, toRejects)

	toAccept = toAccepts[0]
	assert.Equal(t, txC2.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)
}

func TestRejectTwiceAcrossRounds(t *testing.T) {
	c := New()

	conflictInput := ids.GenerateTestID()

	// Regression test to ensure that a transaction cannot be rejected
	// twice across multiple iterations of the worklist. This is prevented
	// by maintaining the set of rejected txIDs across multiple iterations
	// of the worklist.
	trA := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{conflictInput},
	}
	trB := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{conflictInput},
	}
	trC := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID()},
		DependenciesV: []Transition{trA},
	}
	trD := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID()},
		DependenciesV: []Transition{trC},
	}

	txA := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
	}
	txB := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trB,
	}
	txC := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trC,
	}
	txD := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trD,
	}

	err := c.Add(txA)
	assert.NoError(t, err)

	err = c.Add(txB)
	assert.NoError(t, err)

	err = c.Add(txC)
	assert.NoError(t, err)

	err = c.Add(txD)
	assert.NoError(t, err)

	// Accept txB first, such that it is marked as accepted.
	c.Accept(txB.ID())

	// Accepting txB should cause txA to be rejected due to a conflict.
	// Since txA is rejected, txC will be rejected due to a missing dependency.
	// Since trD depends on trC, this will transtiively cause txD to be
	// rejected as well.
	toAccepts, toRejects := c.Updateable()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 3)

	expectedAccepts := ids.Set{}
	expectedAccepts.Add(txB.ID())
	for i, toAccept := range toAccepts {
		err := toAccept.Accept()
		assert.NoError(t, err)
		assert.True(t, expectedAccepts.Contains(toAccept.ID()), "Unexpected accepted txID: %s index %d", toAccept.ID(), i)
	}

	expectedRejects := ids.Set{}
	expectedRejects.Add(txA.ID(), txC.ID(), txD.ID())
	for i, toReject := range toRejects {
		assert.True(t, expectedRejects.Contains(toReject.ID()), "Unexpected rejected txID: %s index %d", toReject.ID(), i)
		err := toReject.Reject()
		assert.NoError(t, err)
	}

	toAccepts, toRejects = c.Updateable()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)
	assert.Equal(t, 0, c.acceptableIDs.Len())
	assert.Equal(t, 0, c.rejectableIDs.Len())
}

func TestRejectTwiceAcrossRoundsEpochs(t *testing.T) {
	c := New()

	conflictInput := ids.GenerateTestID()

	// Transition AB is a bridge dependency from trA
	// to trB ie. trB depends on trAB which depends on trA
	// Transitions B and C conflict over [conflictInput]
	trA := &TestTransition{
		IDV:       ids.GenerateTestID(),
		StatusV:   choices.Processing,
		InputIDsV: []ids.ID{ids.GenerateTestID()},
	}
	trAB := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID()},
		DependenciesV: []Transition{trA},
	}
	trB := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID(), conflictInput},
		DependenciesV: []Transition{trAB},
	}
	trC := &TestTransition{
		IDV:           ids.GenerateTestID(),
		StatusV:       choices.Processing,
		InputIDsV:     []ids.ID{ids.GenerateTestID(), conflictInput},
		DependenciesV: []Transition{trA},
	}

	txA0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
	}
	txA1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trA,
		EpochV:      1,
	}
	txAB0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trAB,
	}
	txB0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trB,
	}
	txC1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: trC,
		EpochV:      1,
	}

	err := c.Add(txA0)
	assert.NoError(t, err)

	err = c.Add(txA1)
	assert.NoError(t, err)

	err = c.Add(txAB0)
	assert.NoError(t, err)

	err = c.Add(txB0)
	assert.NoError(t, err)

	err = c.Add(txC1)
	assert.NoError(t, err)

	// Accept txC1 first, such that it is marked as conditionally
	// acceptable.
	c.Accept(txC1.ID())
	// Accept txA1, such that txC1 will be accepted on the second
	// iteration.
	c.Accept(txA1.ID())

	// Accepting txA1 should cause txA0 (epoch conflict) and txAB0
	// (dependency not met in time) to be rejected.
	// txC1 should be moved from conditionally accepted to accepted
	// and txB0 should be rejected since its direct dependency trAB
	// was rejected in the only round it was issued in.

	toAccepts, toRejects := c.Updateable()
	expectedAccepts := ids.Set{}
	expectedAccepts.Add(txC1.ID(), txA1.ID())

	for i, toAccept := range toAccepts {
		err = toAccept.Accept()
		assert.NoError(t, err)
		assert.True(t, expectedAccepts.Contains(toAccept.ID()), "Unexpected accepted txID: %s index %d", toAccept.ID(), i)
	}

	expectedRejects := ids.Set{}
	expectedRejects.Add(txAB0.ID(), txA0.ID(), txB0.ID())
	for i, toReject := range toRejects {
		assert.True(t, expectedRejects.Contains(toReject.ID()), "Unexpected rejected txID: %s index %d", toReject.ID(), i)
		err = toReject.Reject()
		assert.NoError(t, err)
	}

	toAccepts, toRejects = c.Updateable()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)
	assert.Equal(t, 0, c.acceptableIDs.Len())
	assert.Equal(t, 0, c.rejectableIDs.Len())
}

func TestAcceptRejectedMultipleEpochDependency(t *testing.T) {
	c := New()

	inputIDs := []ids.ID{ids.GenerateTestID()}
	tr := &TestTransition{
		IDV:       ids.GenerateTestID(),
		InputIDsV: inputIDs,
		StatusV:   choices.Processing,
	}
	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr,
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr,
		EpochV:      1,
	}
	tx2 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: tr,
		EpochV:      2,
	}
	tx3 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		TransitionV: &TestTransition{
			IDV:           ids.GenerateTestID(),
			DependenciesV: []Transition{tr},
			StatusV:       choices.Processing,
		},
		EpochV: 1,
	}

	err := c.Add(tx0)
	assert.NoError(t, err)

	err = c.Add(tx1)
	assert.NoError(t, err)

	err = c.Add(tx2)
	assert.NoError(t, err)

	err = c.Add(tx3)
	assert.NoError(t, err)

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Empty(t, toAccepts)
	assert.Empty(t, toRejects)

	c.Accept(tx2.ID())

	toAccepts = c.updateAccepted()
	toRejects = c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 3)
	assert.Empty(t, c.txs)
	assert.Empty(t, c.utxos)
	assert.Empty(t, c.transitionNodes)

	toAccept := toAccepts[0]
	assert.Equal(t, tx2.ID(), toAccept.ID())
	err = toAccept.Accept()
	assert.NoError(t, err)

	for i, toReject := range toRejects {
		err = toReject.Reject()
		assert.NoError(t, err, "Error rejecting the %d rejectable transaction %s", i, toReject.ID())
	}
}
