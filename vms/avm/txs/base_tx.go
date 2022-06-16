// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ UnsignedTx = &BaseTx{}

// BaseTx is the basis of all transactions.
type BaseTx struct {
	avax.Metadata

	avax.BaseTx `serialize:"true"`
}

func (t *BaseTx) InitCtx(ctx *snow.Context) {
	for _, out := range t.Outs {
		out.InitCtx(ctx)
	}
}

// UTXOs returns the UTXOs transaction is producing.
func (t *BaseTx) UTXOs() []*avax.UTXO {
	txID := t.ID()
	utxos := make([]*avax.UTXO, len(t.Outs))
	for i, out := range t.Outs {
		utxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
	}
	return utxos
}

func (t *BaseTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	txFee uint64,
	_ uint64,
	_ int,
) error {
	if t == nil {
		return errNilTx
	}

	if err := t.Metadata.Verify(); err != nil {
		return err
	}
	if err := t.BaseTx.Verify(ctx); err != nil {
		return err
	}

	return avax.VerifyTx(
		txFee,
		txFeeAssetID,
		[][]*avax.TransferableInput{t.Ins},
		[][]*avax.TransferableOutput{t.Outs},
		c,
	)
}

func (t *BaseTx) Visit(v Visitor) error {
	return v.BaseTx(t)
}
