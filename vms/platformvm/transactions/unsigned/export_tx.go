// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unsigned

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeables"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ Tx = &ExportTx{}

	ErrWrongLocktime   = errors.New("wrong locktime reported")
	ErrOverflowExport  = errors.New("overflow when computing export amount + txFee")
	errNoExportOutputs = errors.New("no export outputs")
)

// ExportTx is an unsigned ExportTx
type ExportTx struct {
	BaseTx `serialize:"true"`

	// Which chain to send the funds to
	DestinationChain ids.ID `serialize:"true" json:"destinationChain"`

	// Outputs that are exported to the chain
	ExportedOutputs []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [UnsignedExportTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *ExportTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	for _, out := range tx.ExportedOutputs {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
}

// SyntacticVerify this transaction is well-formed
func (tx *ExportTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.ExportedOutputs) == 0:
		return errNoExportOutputs
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}

	for _, out := range tx.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
		if _, ok := out.Output().(*stakeables.LockOut); ok {
			return ErrWrongLocktime
		}
	}
	if !avax.IsSortedTransferableOutputs(tx.ExportedOutputs, Codec) {
		return ErrOutputsNotSorted
	}

	tx.SyntacticallyVerified = true
	return nil
}
