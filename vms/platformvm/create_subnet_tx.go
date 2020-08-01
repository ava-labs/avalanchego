// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

// UnsignedCreateSubnetTx is an unsigned proposal to create a new subnet
type UnsignedCreateSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Who is authorized to manage this subnet
	Owner verify.Verifiable `serialize:"true"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedCreateSubnetTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedCreateSubnetTx: %w", err)
	}
	return nil
}

// Verify this transaction is well-formed
func (tx *UnsignedCreateSubnetTx) Verify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := verify.All(&tx.BaseTx, tx.Owner); err != nil {
		return err
	}
	if err := syntacticVerifySpend(tx.Ins, tx.Outs, nil, 0, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify returns nil if [tx] is valid given the state in [db]
func (tx *UnsignedCreateSubnetTx) SemanticVerify(
	db database.Database,
	stx *DecisionTx,
) (
	func() error,
	TxError,
) {
	// Make sure this transaction is well formed.
	if err := tx.Verify(); err != nil {
		return nil, permError{err}
	}

	// Add new subnet to list of subnets
	subnets, err := tx.vm.getSubnets(db)
	if err != nil {
		return nil, tempError{err}
	}
	subnets = append(subnets, stx) // add new subnet
	if err := tx.vm.putSubnets(db, subnets); err != nil {
		return nil, tempError{err}
	}

	// Verify the flowcheck
	if err := tx.vm.semanticVerifySpend(db, tx, tx.Ins, tx.Outs, stx.Credentials); err != nil {
		return nil, err
	}

	txID := tx.ID()

	// Consume the UTXOS
	if err := tx.vm.consumeInputs(db, tx.Ins); err != nil {
		return nil, tempError{err}
	}
	// Produce the UTXOS
	if err := tx.vm.produceOutputs(db, txID, tx.Outs); err != nil {
		return nil, tempError{err}
	}

	// Register new subnet in validator manager
	onAccept := func() error {
		tx.vm.validators.PutValidatorSet(tx.id, validators.NewSet())
		return nil
	}
	return onAccept, nil
}

// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
func (vm *VM) newCreateSubnetTx(
	threshold uint32, // [threshold] of [ownerAddrs] needed to manage this subnet
	ownerAddrs []ids.ShortID, // control addresses for the new subnet
	keys []*crypto.PrivateKeySECP256K1R, // pay the fee
) (*DecisionTx, error) {
	ins, outs, _, signers, err := vm.spend(vm.DB, keys, 0, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Sort control addresses
	ids.SortShortIDs(ownerAddrs)

	// Create the tx
	utx := &UnsignedCreateSubnetTx{
		BaseTx: BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		},
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}
	tx := &DecisionTx{UnsignedDecisionTx: utx}
	if err := vm.signDecisionTx(tx, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify()
}
