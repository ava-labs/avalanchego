// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var _ states.Chain = (*chainState)(nil)

// chainState wraps the disk state and filters non-accepted transactions from
// being returned in GetTx.
type chainState struct {
	states.State
}

func (s *chainState) GetTx(txID ids.ID) (*txs.Tx, error) {
	tx, err := s.State.GetTx(txID)
	if err != nil {
		return nil, err
	}

	// Before the linearization, transactions were persisted before they were
	// marked as Accepted. However, this function aims to only return accepted
	// transactions.
	status, err := s.State.GetStatus(txID)
	if err == database.ErrNotFound {
		// If the status wasn't persisted, then the transaction was written
		// after the linearization, and is accepted.
		return tx, nil
	}
	if err != nil {
		return nil, err
	}

	// If the status was persisted, then the transaction was written before the
	// linearization. If it wasn't marked as accepted, then we treat it as if it
	// doesn't exist.
	if status != choices.Accepted {
		return nil, database.ErrNotFound
	}
	return tx, nil
}
