package transactions

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	ErrNilTx                 = errors.New("tx is nil")
	ErrOutputsNotSorted      = errors.New("outputs not sorted")
	ErrInputsNotSortedUnique = errors.New("inputs not sorted and unique")
)

// BaseTx contains fields common to many transactions.types. It should be
// embedded in transactions.implementations.
type BaseTx struct {
	avax.BaseTx `serialize:"true" json:"inputs"`

	// true iff this transactions.has already passed syntactic verification
	SyntacticallyVerified bool
}

// Verify returns nil iff this tx is well formed
func (tx *BaseTx) Verify(ctx *snow.Context, c codec.Manager) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}
	if err := tx.MetadataVerify(ctx); err != nil {
		return fmt.Errorf("metadata failed verification: %w", err)
	}
	for _, out := range tx.Outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
	}
	for _, in := range tx.Ins {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("input failed verification: %w", err)
		}
	}
	switch {
	case !avax.IsSortedTransferableOutputs(tx.Outs, c):
		return ErrOutputsNotSorted
	case !avax.IsSortedAndUniqueTransferableInputs(tx.Ins):
		return ErrInputsNotSortedUnique
	default:
		return nil
	}
}
