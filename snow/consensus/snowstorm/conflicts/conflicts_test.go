// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

type Test struct {
	Name  string
	Steps []Step
}

type Step struct {
	Add           []Tx
	Processing    []ids.ID
	NotProcessing []ids.ID
	IsVirtuous    []Tx
	IsNotVirtuous []Tx
	Conflicts     []TxConflicts
	Accept        []ids.ID
	Acceptable    []ids.ID
	Rejectable    []ids.ID
	ShouldBeEmpty bool
}

type TxConflicts struct {
	Tx        Tx
	Conflicts []ids.ID
}

func TestVectors(t *testing.T) {
	inputID0 := ids.GenerateTestID()

	trAID := ids.GenerateTestID()
	trA := &TestTransition{IDV: trAID}
	trBID := ids.GenerateTestID()
	trB := &TestTransition{
		IDV:       trBID,
		InputIDsV: []ids.ID{inputID0},
	}
	trCID := ids.GenerateTestID()
	trC := &TestTransition{
		IDV:       trCID,
		InputIDsV: []ids.ID{inputID0},
	}
	trs := []*TestTransition{
		trA,
		trB,
		trC,
	}

	txAEpoch0ID := ids.GenerateTestID()
	txAEpoch0 := &TestTx{
		TestDecidable: choices.TestDecidable{IDV: txAEpoch0ID},
		TransitionV:   trA,
	}
	txBEpoch0ID := ids.GenerateTestID()
	txBEpoch0 := &TestTx{
		TestDecidable: choices.TestDecidable{IDV: txBEpoch0ID},
		TransitionV:   trB,
	}
	txCEpoch0ID := ids.GenerateTestID()
	txCEpoch0 := &TestTx{
		TestDecidable: choices.TestDecidable{IDV: txCEpoch0ID},
		TransitionV:   trC,
	}
	txARestCEpoch1ID := ids.GenerateTestID()
	txARestCEpoch1 := &TestTx{
		TestDecidable: choices.TestDecidable{IDV: txARestCEpoch1ID},
		TransitionV:   trA,
		EpochV:        1,
		RestrictionsV: []ids.ID{trCID},
	}
	txs := []*TestTx{
		txAEpoch0,
		txARestCEpoch1,
		txBEpoch0,
		txCEpoch0,
	}

	tests := []struct {
		Name  string
		Steps []Step
	}{
		{
			Name: "adding changes processing",
			Steps: []Step{
				{
					NotProcessing: []ids.ID{
						trAID,
					},
				},
				{
					Add: []Tx{
						txAEpoch0,
					},
					Processing: []ids.ID{
						trAID,
					},
				},
			},
		},
		{
			Name: "correctly marked as virtuous",
			Steps: []Step{
				{
					IsVirtuous: []Tx{
						txAEpoch0,
					},
				},
				{
					Add: []Tx{
						txAEpoch0,
					},
					IsVirtuous: []Tx{
						txAEpoch0,
					},
				},
			},
		},
		{
			Name: "correctly marked as rogue",
			Steps: []Step{
				{
					IsVirtuous: []Tx{
						txBEpoch0,
						txCEpoch0,
					},
					Conflicts: []TxConflicts{
						{
							Tx: txBEpoch0,
						},
						{
							Tx: txCEpoch0,
						},
					},
				},
				{
					Add: []Tx{
						txBEpoch0,
					},
					IsVirtuous: []Tx{
						txBEpoch0,
					},
					IsNotVirtuous: []Tx{
						txCEpoch0,
					},
					Conflicts: []TxConflicts{
						{
							Tx: txBEpoch0,
						},
						{
							Tx: txCEpoch0,
							Conflicts: []ids.ID{
								txBEpoch0ID,
							},
						},
					},
				},
			},
		},
		{
			Name: "restriction would be conflicting",
			Steps: []Step{
				{
					IsVirtuous: []Tx{
						txCEpoch0,
						txARestCEpoch1,
					},
					Conflicts: []TxConflicts{
						{
							Tx: txCEpoch0,
						},
						{
							Tx: txARestCEpoch1,
						},
					},
				},
				{
					Add: []Tx{
						txCEpoch0,
					},
					IsVirtuous: []Tx{
						txCEpoch0,
					},
					IsNotVirtuous: []Tx{
						txARestCEpoch1,
					},
					Conflicts: []TxConflicts{
						{
							Tx: txCEpoch0,
						},
						{
							Tx: txARestCEpoch1,
							Conflicts: []ids.ID{
								txCEpoch0ID,
							},
						},
					},
				},
			},
		},
		{
			Name: "restriction is conflicting",
			Steps: []Step{
				{
					IsVirtuous: []Tx{
						txARestCEpoch1,
						txCEpoch0,
					},
					Conflicts: []TxConflicts{
						{
							Tx: txARestCEpoch1,
						},
						{
							Tx: txCEpoch0,
						},
					},
				},
				{
					Add: []Tx{
						txARestCEpoch1,
					},
					IsVirtuous: []Tx{
						txARestCEpoch1,
					},
					IsNotVirtuous: []Tx{
						txCEpoch0,
					},
					Conflicts: []TxConflicts{
						{
							Tx: txARestCEpoch1,
						},
						{
							Tx: txCEpoch0,
							Conflicts: []ids.ID{
								txARestCEpoch1ID,
							},
						},
					},
				},
			},
		},
		{
			Name: "accept no conflicts",
			Steps: []Step{
				{
					Add: []Tx{
						txAEpoch0,
					},
					Accept: []ids.ID{
						txAEpoch0ID,
					},
					Acceptable: []ids.ID{
						txAEpoch0ID,
					},
					Rejectable:    []ids.ID{},
					ShouldBeEmpty: true,
				},
				{
					Add:           []Tx(nil),
					Processing:    []ids.ID(nil),
					NotProcessing: []ids.ID(nil),
					IsVirtuous:    []Tx(nil),
					IsNotVirtuous: []Tx(nil),
					Conflicts:     []TxConflicts(nil),
					Accept:        []ids.ID(nil),
					Acceptable:    []ids.ID(nil),
					Rejectable:    []ids.ID(nil),
					ShouldBeEmpty: false,
				},
			},
		},
	}
	for _, test := range tests {
		for _, tr := range trs {
			tr.StatusV = choices.Processing
			tr.EpochV = 0
		}
		for _, tx := range txs {
			tx.StatusV = choices.Processing
		}

		t.Run(test.Name, func(t *testing.T) {
			c := New()

			for _, step := range test.Steps {
				for _, tx := range step.Add {
					c.Add(tx)
				}
				for _, trID := range step.Processing {
					processing := c.Processing(trID)
					assert.True(t, processing, "transition %s should have been processing", trID)
				}
				for _, trID := range step.NotProcessing {
					processing := c.Processing(trID)
					assert.False(t, processing, "transition %s shouldn't have been processing", trID)
				}
				for _, tx := range step.IsVirtuous {
					isVirtuous := c.IsVirtuous(tx)
					assert.True(t, isVirtuous, "tx %s should have been virtuous", tx.ID())
				}
				for _, tx := range step.IsNotVirtuous {
					isVirtuous := c.IsVirtuous(tx)
					assert.False(t, isVirtuous, "tx %s shouldn't have been virtuous", tx.ID())
				}
				for _, conf := range step.Conflicts {
					conflicts := c.Conflicts(conf.Tx)
					conflictsTxIDs := make([]ids.ID, len(conflicts))
					for i, tx := range conflicts {
						conflictsTxIDs[i] = tx.ID()
					}
					assert.ElementsMatch(t, conf.Conflicts, conflictsTxIDs, "wrong transactions marked as conflicting")
				}
				for _, txID := range step.Accept {
					c.Accept(txID)
				}
				acceptable, rejectable := c.Updateable()
				acceptableTxIDs := make([]ids.ID, len(acceptable))
				for i, tx := range acceptable {
					err := tx.Accept()
					assert.NoError(t, err)

					acceptableTxIDs[i] = tx.ID()
				}
				rejectableTxIDs := make([]ids.ID, len(rejectable))
				for i, tx := range rejectable {
					err := tx.Reject()
					assert.NoError(t, err)

					rejectableTxIDs[i] = tx.ID()
				}

				if step.Acceptable != nil {
					assert.Equal(t, step.Acceptable, acceptableTxIDs, "wrong transactions or order marked as acceptable")
				}
				if step.Rejectable != nil {
					assert.ElementsMatch(t, step.Rejectable, rejectableTxIDs, "wrong transactions marked as rejectable")
				}

				if step.ShouldBeEmpty {
					assert.Empty(t, c.txs)
					assert.Empty(t, c.transitionNodes)
					assert.Empty(t, c.utxos)
					assert.Empty(t, c.conditionallyAccepted)
					assert.Empty(t, c.acceptableIDs)
					assert.Empty(t, c.acceptable)
					assert.Empty(t, c.rejectableIDs)
					assert.Empty(t, c.rejectable)
				}
			}
		})
	}
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

	c.Add(tx0)
	c.Add(tx1)

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
	err := toAccept.Accept()
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

	c.Add(tx0)
	c.Add(tx1)

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
	err := toAccept.Accept()
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

	c.Add(tx0)
	c.Add(tx1)
	c.Add(tx2)

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
	err := toAccept.Accept()
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

	c.Add(tx0)
	c.Add(tx1)
	c.Add(tx2)

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
	err := toAccept.Accept()
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

	c.Add(tx0)
	c.Add(tx1)
	c.Add(tx2)

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
	err := toAccept.Accept()
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

	c.Add(txA0)
	c.Add(txA1)
	c.Add(txB0)
	c.Add(txB1)
	c.Add(txC0)
	c.Add(txC1)

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
	err := toAccept.Accept()
	assert.NoError(t, err)

	for i, toReject := range toRejects {
		err := toReject.Reject()
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

	c.Add(txAY)
	c.Add(txAX)
	c.Add(txBY)
	c.Add(txBX)

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
	err := toAccept.Accept()
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

	c.Add(txAX)
	c.Add(txAY)
	c.Add(txBX)
	c.Add(txBY)

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
	err := toAccept.Accept()
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

	c.Add(txA0)
	c.Add(txA1)
	c.Add(txA2)
	c.Add(txB0)
	c.Add(txB1)
	c.Add(txB2)
	c.Add(txC0)
	c.Add(txC1)
	c.Add(txC2)

	c.Accept(txA2.ID())

	toAccepts := c.updateAccepted()
	toRejects := c.updateRejected()
	assert.Len(t, toAccepts, 1)
	assert.Len(t, toRejects, 4)

	toAccept := toAccepts[0]
	assert.Equal(t, txA2.ID(), toAccept.ID())
	err := toAccept.Accept()
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

	c.Add(txA)
	c.Add(txB)
	c.Add(txC)
	c.Add(txD)

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

	c.Add(txA0)
	c.Add(txA1)
	c.Add(txAB0)
	c.Add(txB0)
	c.Add(txC1)

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
		err := toAccept.Accept()
		assert.NoError(t, err)
		assert.True(t, expectedAccepts.Contains(toAccept.ID()), "Unexpected accepted txID: %s index %d", toAccept.ID(), i)
	}

	expectedRejects := ids.Set{}
	expectedRejects.Add(txAB0.ID(), txA0.ID(), txB0.ID())
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

	c.Add(tx0)
	c.Add(tx1)
	c.Add(tx2)
	c.Add(tx3)

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
	err := toAccept.Accept()
	assert.NoError(t, err)

	for i, toReject := range toRejects {
		err := toReject.Reject()
		assert.NoError(t, err, "Error rejecting the %d rejectable transaction %s", i, toReject.ID())
	}
}
