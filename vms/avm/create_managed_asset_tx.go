package avm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// CreateManagedAssetTx creates an asset such that each UTXO with this
// asset ID may be consumed by the manager of the asset
type CreateManagedAssetTx struct {
	CreateAssetTx `serialize:"true"`
}

// SyntacticVerify that this transaction is well-formed.
func (t *CreateManagedAssetTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Manager,
	codecVersion uint16,
	txFeeAssetID ids.ID,
	txFee uint64,
	creationTxFee uint64,
	numFxs int,
) error {
	if t == nil {
		return errNilTx
	}
	err := t.CreateAssetTx.SyntacticVerify(
		ctx,
		c,
		codecVersion,
		txFeeAssetID,
		txFee,
		creationTxFee,
		numFxs,
	)
	if err != nil {
		return err
	}
	initialStates := t.InitialStates()
	if len(initialStates) != 1 {
		return fmt.Errorf(
			"should have 1 element in initial states but have %d",
			len(initialStates),
		)
	}
	initialState := initialStates[0]
	if initialState.FxID != 0 { // TODO lookup secp256k1fx ID
		return fmt.Errorf(
			"expected initial state to be secp256k1 (0) but is %d",
			initialState.FxID,
		)
	}
	if len(initialState.Outs) != 1 {
		return fmt.Errorf(
			"should have 1 element in initial state but have %d",
			len(initialState.Outs),
		)
	}
	if _, ok := initialState.Outs[0].(*secp256k1fx.ManagedAssetStatusOutput); !ok {
		return fmt.Errorf(
			"initial state output should be (*secp256k1fx.ManagedAssetStatusOutput but is %T",
			initialStates[0],
		)
	}
	return nil
}
