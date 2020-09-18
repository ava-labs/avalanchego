// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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
	c codec.Codec,
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
	db database.Database,
	stx *Tx,
) (
	func() error,
	TxError,
) {
	// Make sure this transaction is well formed.
	if err := tx.Verify(vm.Ctx, vm.codec, vm.creationTxFee, vm.Ctx.AVAXAssetID); err != nil {
		return nil, permError{err}
	}

	// Add new subnet to list of subnets
	subnets, err := vm.getSubnets(db)
	if err != nil {
		return nil, tempError{err}
	}
	subnets = append(subnets, stx) // add new subnet
	if err := vm.putSubnets(db, subnets); err != nil {
		return nil, tempError{err}
	}

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(db, tx, tx.Ins, tx.Outs, stx.Creds, vm.creationTxFee, vm.Ctx.AVAXAssetID); err != nil {
		return nil, err
	}

	txID := tx.ID()

	// Consume the UTXOS
	if err := vm.consumeInputs(db, tx.Ins); err != nil {
		return nil, tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(db, txID, tx.Outs); err != nil {
		return nil, tempError{err}
	}
	// Register new subnet in validator manager
	onAccept := func() error {
		return vm.vdrMgr.Set(tx.ID(), validators.NewSet())
	}
	return onAccept, nil
}

// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
func (vm *VM) newCreateSubnetTx(
	threshold uint32, // [threshold] of [ownerAddrs] needed to manage this subnet
	ownerAddrs []ids.ShortID, // control addresses for the new subnet
	keys []*crypto.PrivateKeySECP256K1R, // pay the fee
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*Tx, error) {
	ins, outs, _, signers, err := vm.stake(vm.DB, keys, 0, vm.creationTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Sort control addresses
	ids.SortShortIDs(ownerAddrs)

	// Create the tx
	utx := &UnsignedCreateSubnetTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
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
	return tx, utx.Verify(vm.Ctx, vm.codec, vm.creationTxFee, vm.Ctx.AVAXAssetID)
}
