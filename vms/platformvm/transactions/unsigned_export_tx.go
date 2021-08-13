// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/entities"
	"github.com/ava-labs/avalanchego/vms/platformvm/platformcodec"
)

var (
	ErrWrongLocktime   = errors.New("wrong locktime reported")
	errNoExportOutputs = errors.New("no export outputs")
)

// UnsignedExportTx is an unsigned ExportTx
type UnsignedExportTx struct {
	BaseTx `serialize:"true"`

	// Which chain to send the funds to
	DestinationChain ids.ID `serialize:"true" json:"destinationChain"`

	// Outputs that are exported to the chain
	ExportedOutputs []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// InputUTXOs returns an empty set
func (tx *UnsignedExportTx) InputUTXOs() ids.Set { return ids.Set{} }

// Verify this is well-formed
func (tx *UnsignedExportTx) Verify(
	avmID ids.ID,
	ctx *snow.Context,
	c codec.Manager,
	feeAmount uint64,
	feeAssetID ids.ID,
) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.DestinationChain != avmID:
		// TODO: remove this check if we allow for P->C swaps
		return ErrWrongChainID
	case len(tx.ExportedOutputs) == 0:
		return errNoExportOutputs
	}

	if err := tx.BaseTx.Verify(ctx, platformcodec.Codec); err != nil {
		return err
	}

	for _, out := range tx.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
		if _, ok := out.Output().(*entities.StakeableLockOut); ok {
			return ErrWrongLocktime
		}
	}
	if !avax.IsSortedTransferableOutputs(tx.ExportedOutputs, platformcodec.Codec) {
		return ErrOutputsNotSorted
	}

	tx.SyntacticallyVerified = true
	return nil
}
