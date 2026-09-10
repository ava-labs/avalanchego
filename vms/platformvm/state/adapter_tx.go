// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// Tx pairs a staker transaction body of type T with the ID of the signed
// transaction that carries it. A transaction's ID is a hash of its signed
// bytes, so it cannot be recomputed from the body alone; the only source of a
// correctly paired ID is the signed transaction itself. Construction is
// restricted to [NewTx] so the pairing cannot be forged.
type Tx[T platform.Staker] struct {
	id   ids.ID
	body T
}

// NewTx returns tx's body as a T, paired with tx's ID. It returns an error if
// tx's body is not a T.
func NewTx[T platform.Staker](tx *platform.Tx) (Tx[T], error) {
	if tx == nil {
		return Tx[T]{}, fmt.Errorf("%w: nil transaction", errUnexpectedStaker)
	}

	body, ok := tx.Unsigned.(T)
	if !ok {
		return Tx[T]{}, fmt.Errorf("%w: %T", errUnexpectedStaker, tx.Unsigned)
	}

	return Tx[T]{id: tx.ID(), body: body}, nil
}

// ID returns the ID of the signed transaction the body was taken from.
func (t Tx[T]) ID() ids.ID {
	return t.id
}

// Body returns the transaction body.
func (t Tx[T]) Body() T {
	return t.body
}
