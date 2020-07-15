// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

const maxThreshold = 25

var (
	errThresholdExceedsKeysLen       = errors.New("threshold must be no more than number of control keys")
	errThresholdTooHigh              = fmt.Errorf("threshold can't be greater than %d", maxThreshold)
	errControlKeysNotSortedAndUnique = errors.New("control keys must be sorted and unique")
	errUnneededKeys                  = errors.New("subnets shouldn't have keys if the threshold is 0")
)

// UnsignedCreateSubnetTx is an unsigned proposal to create a new subnet
type UnsignedCreateSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Each element in ControlKeys is the address of a public key
	// In order to add a validator to this subnet, a tx must be signed
	// with Threshold of these keys
	ControlKeys []ids.ShortID `serialize:"true"`
	// See ControlKeys
	Threshold uint16 `serialize:"true"`
}

// CreateSubnetTx is a proposal to create a new subnet
type CreateSubnetTx struct {
	UnsignedCreateSubnetTx `serialize:"true"`
	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *CreateSubnetTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *CreateSubnetTx) initialize(vm *VM) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedCreateSubnetTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedCreateSubnetTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal CreateSubnetTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return err
}

// SyntacticVerify nil iff [tx] is syntactically valid.
// If [tx] is valid, this method sets [tx.key]
func (tx *CreateSubnetTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.id.IsZero():
		return errInvalidID
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return errWrongNetworkID
	case tx.Threshold > uint16(len(tx.ControlKeys)):
		return errThresholdExceedsKeysLen
	case tx.Threshold > maxThreshold:
		return errThresholdTooHigh
	case tx.Threshold == 0 && len(tx.ControlKeys) > 0:
		return errUnneededKeys
	case !ids.IsSortedAndUniqueShortIDs(tx.ControlKeys):
		return errControlKeysNotSortedAndUnique
	}
	if err := syntacticVerifySpend(tx, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify returns nil if [tx] is valid given the state in [db]
func (tx *CreateSubnetTx) SemanticVerify(db database.Database) (func(), error) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, err
	}

	// Add new subnet to list of subnets
	subnets, err := tx.vm.getSubnets(db)
	if err != nil {
		return nil, err
	}
	subnets = append(subnets, tx) // add new subnet
	if err := tx.vm.putSubnets(db, subnets); err != nil {
		return nil, err
	}

	// Verify inputs/outputs and update the UTXO set
	if err := tx.vm.semanticVerifySpend(db, tx); err != nil {
		return nil, tempError{fmt.Errorf("couldn't verify tx: %w", err)}
	}

	// Register new subnet in validator manager
	onAccept := func() {
		tx.vm.validators.PutValidatorSet(tx.id, validators.NewSet())
	}
	return onAccept, nil
}

// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
func (vm *VM) newCreateSubnetTx(
	controlKeys []ids.ShortID, // Control keys for the new subnet
	threshold uint16, // [threshold] of [controlKeys] signatures needed to add validator to this subnet
	keys []*crypto.PrivateKeySECP256K1R, // Pay the fee
) (*CreateSubnetTx, error) {

	if int(threshold) > len(controlKeys) {
		return nil, fmt.Errorf("threshold (%d) > len(controlKeys) (%d)", threshold, len(controlKeys))
	}

	// Calculate inputs, outputs, and keys used to sign this tx
	inputs, outputs, credKeys, err := vm.spend(vm.DB, vm.txFee, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the tx
	tx := &CreateSubnetTx{
		UnsignedCreateSubnetTx: UnsignedCreateSubnetTx{
			BaseTx: BaseTx{
				NetworkID:    vm.Ctx.NetworkID,
				BlockchainID: vm.Ctx.ChainID,
				Inputs:       inputs,
				Outputs:      outputs,
			},
			ControlKeys: controlKeys,
			Threshold:   threshold,
		},
	}
	// Sort control keys
	ids.SortShortIDs(tx.ControlKeys)
	// Ensure control keys are unique and sorted
	if !ids.IsSortedAndUniqueShortIDs(tx.ControlKeys) {
		return nil, errControlKeysNotSortedAndUnique
	}

	// Generate byte repr. of unsigned tx
	if tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedCreateSubnetTx)); err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedAddNonDefaultSubnetValidatorTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// Attach credentials that allow the inputs to be spent
	for _, inputKeys := range credKeys { // [inputKeys] are the keys used to authorize spend of an input
		cred := &secp256k1fx.Credential{}
		for _, key := range inputKeys {
			sig, err := key.SignHash(hash) // Sign hash(tx.unsignedBytes)
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.Credentials = append(tx.Credentials, cred) // Attach credential to tx
	}

	return tx, tx.initialize(vm)
}
