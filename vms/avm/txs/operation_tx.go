// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errOperationsNotSortedUnique = errors.New("operations not sorted and unique")
	errNoOperations              = errors.New("an operationTx must have at least one operation")
	errDoubleSpend               = errors.New("inputs attempt to double spend an input")

	_ UnsignedTx             = (*OperationTx)(nil)
	_ secp256k1fx.UnsignedTx = (*OperationTx)(nil)
)

// OperationTx is a transaction with no credentials.
type OperationTx struct {
	BaseTx `serialize:"true"`

	Ops []*Operation `serialize:"true" json:"operations"`
}

func (t *OperationTx) InitCtx(ctx *snow.Context) {
	for _, op := range t.Ops {
		op.Op.InitCtx(ctx)
	}
	t.BaseTx.InitCtx(ctx)
}

// Operations track which ops this transaction is performing. The returned array
// should not be modified.
func (t *OperationTx) Operations() []*Operation {
	return t.Ops
}

func (t *OperationTx) InputUTXOs() []*avax.UTXOID {
	utxos := t.BaseTx.InputUTXOs()
	for _, op := range t.Ops {
		utxos = append(utxos, op.UTXOIDs...)
	}
	return utxos
}

func (t *OperationTx) InputIDs() set.Set[ids.ID] {
	inputs := t.BaseTx.InputIDs()
	for _, op := range t.Ops {
		for _, utxo := range op.UTXOIDs {
			inputs.Add(utxo.InputID())
		}
	}
	return inputs
}

// ConsumedAssetIDs returns the IDs of the assets this transaction consumes
func (t *OperationTx) ConsumedAssetIDs() set.Set[ids.ID] {
	assets := t.AssetIDs()
	for _, op := range t.Ops {
		if len(op.UTXOIDs) > 0 {
			assets.Add(op.AssetID())
		}
	}
	return assets
}

// AssetIDs returns the IDs of the assets this transaction depends on
func (t *OperationTx) AssetIDs() set.Set[ids.ID] {
	assets := t.BaseTx.AssetIDs()
	for _, op := range t.Ops {
		assets.Add(op.AssetID())
	}
	return assets
}

// NumCredentials returns the number of expected credentials
func (t *OperationTx) NumCredentials() int {
	return t.BaseTx.NumCredentials() + len(t.Ops)
}

// SyntacticVerify that this transaction is well-formed.
func (t *OperationTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	config *config.Config,
	numFxs int,
) error {
	switch {
	case t == nil:
		return errNilTx
	case len(t.Ops) == 0:
		return errNoOperations
	}

	if err := t.BaseTx.SyntacticVerify(ctx, c, txFeeAssetID, config, numFxs); err != nil {
		return err
	}

	inputs := set.NewSet[ids.ID](len(t.Ins))
	for _, in := range t.Ins {
		inputs.Add(in.InputID())
	}

	for _, op := range t.Ops {
		if err := op.Verify(); err != nil {
			return err
		}
		for _, utxoID := range op.UTXOIDs {
			inputID := utxoID.InputID()
			if inputs.Contains(inputID) {
				return errDoubleSpend
			}
			inputs.Add(inputID)
		}
	}
	if !IsSortedAndUniqueOperations(t.Ops, c) {
		return errOperationsNotSortedUnique
	}
	return nil
}

func (t *OperationTx) Visit(v Visitor) error {
	return v.OperationTx(t)
}
