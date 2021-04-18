// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ UnsignedDecisionTx = &UnsignedCreateSubnetTx{}
)

// UnsignedCreateSubnetTx is an unsigned proposal to create a new subnet
type UnsignedCreateSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Who is authorized to manage this subnet
	Owner verify.Verifiable `serialize:"true" json:"owner"`
}

// Verify this transaction is well-formed
func (tx *UnsignedCreateSubnetTx) Verify(
	ctx *snow.Context,
	c codec.Manager,
	feeAmount uint64,
	feeAssetID ids.ID,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}
	if err := tx.Owner.Verify(); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify returns nil if [tx] is valid given the state in [db]
func (tx *UnsignedCreateSubnetTx) SemanticVerify(
	vm *VM,
	vs versionedState,
	stx *Tx,
) (
	func() error,
	TxError,
) {
	// Make sure this transaction is well formed.
	if err := tx.Verify(vm.ctx, vm.codec, vm.CreationTxFee, vm.ctx.AVAXAssetID); err != nil {
		return nil, permError{err}
	}

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(vs, tx, tx.Ins, tx.Outs, stx.Creds, vm.CreationTxFee, vm.ctx.AVAXAssetID); err != nil {
		return nil, err
	}

	// Consume the UTXOS
	vm.consumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	vm.produceOutputs(vs, txID, tx.Outs)
	// Attempt to the new chain to the database
	vs.AddSubnet(stx)

	return nil, nil
}

/*
// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
func (vm *VM) newCreateSubnetTx(
	threshold uint32, // [threshold] of [ownerAddrs] needed to manage this subnet
	ownerAddrs []ids.ShortID, // control addresses for the new subnet
	keys []*crypto.PrivateKeySECP256K1R, // pay the fee
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*Tx, error) {
	ins, outs, _, signers, err := vm.stake(vm.DB, keys, 0, vm.CreationTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Sort control addresses
	ids.SortShortIDs(ownerAddrs)

	// Create the tx
	utx := &UnsignedCreateSubnetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.ctx, vm.codec, vm.CreationTxFee, vm.ctx.AVAXAssetID)
}
*/
