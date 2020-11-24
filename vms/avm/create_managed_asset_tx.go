package avm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

// CreateManagedAssetTx creates an asset such that each UTXO with this
// asset ID may be consumed by the creator of the asset
type CreateManagedAssetTx struct {
	CreateAssetTx

	// Owner has permission to spend UTXOs of this asset
	Owner verify.Verifiable
}

// SyntacticVerify that this transaction is well-formed.
func (t *CreateManagedAssetTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Codec,
	txFeeAssetID ids.ID,
	txFee uint64,
	creationTxFee uint64,
	numFxs int,
) error {
	if t == nil {
		return errNilTx
	}
	if err := t.CreateAssetTx.SyntacticVerify(ctx, c, txFeeAssetID, txFee, creationTxFee, numFxs); err != nil {
		return err
	}
	return t.Owner.Verify()
}
