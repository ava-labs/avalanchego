// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/events"
)

type Conflicts interface {
	// Add this transaction to conflict tracking
	Add(tx choices.Decidable) error

	// Conflicts returns true if there are no transactions currently tracked
	IsVirtuous(tx choices.Decidable) (bool, error)

	// Conflicts returns the transactions that conflict with the provided
	// transaction
	Conflicts(tx choices.Decidable) ([]choices.Decidable, error)

	// Mark this transaction as conditionally accepted
	Accept(txID ids.ID)

	// Updateable returns the transactions that can be accepted and rejected.
	// Assumes that returned transactions are accepted or rejected before the
	// next time this function is called. Acceptable transactions must have been
	// identified as having been confitionally accepted. If an acceptable
	// transaction was marked as having a conflict, then that conflict should be
	// returned in the same call as the acceptable transaction was returned or
	// in a prior call.
	Updateable() (acceptable []choices.Decidable, rejectable []choices.Decidable)
}

var (
	errInvalidTxType = errors.New("invalid tx type")

	_ Conflicts = &conflicts{}
)

type conflicts struct {
	txs   map[[32]byte]Tx
	utxos map[[32]byte]ids.Set

	// keeps track of whether dependencies have been accepted
	pendingAccept events.Blocker

	// keeps track of whether dependencies have been rejected
	pendingReject events.Blocker

	acceptable []choices.Decidable
	rejectable []choices.Decidable
}

func (c *conflicts) Add(txIntf choices.Decidable) error {
	tx, ok := txIntf.(Tx)
	if !ok {
		return errInvalidTxType
	}

	txID := tx.ID()
	c.txs[txID.Key()] = tx
	for inputKey := range tx.InputIDs() {
		spenders := c.utxos[inputKey]
		spenders.Add(txID)
		c.utxos[inputKey] = spenders
	}
	return nil
}

func (c *conflicts) IsVirtuous(txIntf choices.Decidable) (bool, error) {
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
	return true, nil
}

func (c *conflicts) Conflicts(txIntf choices.Decidable) ([]choices.Decidable, error) {
	tx, ok := txIntf.(Tx)
	if !ok {
		return nil, errInvalidTxType
	}

	txID := tx.ID()
	var conflictSet ids.Set
	conflictSet.Add(txID)

	conflicts := []choices.Decidable(nil)
	for inputKey := range tx.InputIDs() {
		spenders := c.utxos[inputKey]
		for spenderKey := range spenders {
			spenderID := ids.NewID(spenderKey)
			if conflictSet.Contains(spenderID) {
				continue
			}
			conflictSet.Add(spenderID)
			conflicts = append(conflicts, c.txs[spenderKey])
		}
	}
	return conflicts, nil
}

func (c *conflicts) Accept(txID ids.ID) {}

func (c *conflicts) Updateable() (acceptable []choices.Decidable, rejectable []choices.Decidable) {
	return nil, nil
}

// acceptor implements Blockable
type acceptor struct {
	c        *conflicts
	deps     ids.Set
	rejected bool
	tx       Tx
}

func (a *acceptor) Dependencies() ids.Set { return a.deps }

func (a *acceptor) Fulfill(id ids.ID) {
	a.deps.Remove(id)
	a.Update()
}

func (a *acceptor) Abandon(id ids.ID) { a.rejected = true }

func (a *acceptor) Update() {
	// If I was rejected or I am still waiting on dependencies to finish or an
	// error has occurred, I shouldn't do anything.
	if a.rejected || a.deps.Len() != 0 {
		return
	}
	a.c.acceptable = append(a.c.acceptable, a.tx)
}

// rejector implements Blockable
type rejector struct {
	c        *conflicts
	deps     ids.Set
	rejected bool // true if the tx has been rejected
	tx       Tx
}

func (r *rejector) Dependencies() ids.Set { return r.deps }

func (r *rejector) Fulfill(ids.ID) {
	if r.rejected {
		return
	}
	r.rejected = true
	r.c.rejectable = append(r.c.rejectable, r.tx)
}

func (*rejector) Abandon(ids.ID) {}
func (*rejector) Update()        {}
