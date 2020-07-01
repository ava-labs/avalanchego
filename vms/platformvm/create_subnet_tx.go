// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
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
	vm *VM

	// Metadata about this transaction
	Metadata `serialize:"true"`

	// Each element in ControlKeys is the address of a public key
	// In order to add a validator to this subnet, a tx must be signed
	// with Threshold of these keys
	ControlKeys []ids.ShortID `serialize:"true"`

	// See ControlKeys
	Threshold uint16 `serialize:"true"`

	// Input UTXOs
	Ins []*ava.TransferableInput `serialize:"true"`

	// Output UTXOs
	Outs []*ava.TransferableOutput `serialize:"true"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *UnsignedCreateSubnetTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// CreateSubnetTx is a proposal to create a new subnet
type CreateSubnetTx struct {
	UnsignedCreateSubnetTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Creds []verify.Verifiable `serialize:"true"`
}

// initialize sets [tx.vm] to [vm]
func (tx *CreateSubnetTx) initialize(vm *VM) error {
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

// ID returns the ID of this transaction
func (tx *CreateSubnetTx) ID() ids.ID { return tx.id }

// SyntacticVerify nil iff [tx] is syntactically valid.
// If [tx] is valid, this method sets [tx.key]
// TODO: Only verify once
func (tx *CreateSubnetTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
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

	if err := syntacticVerifySpend(tx.Ins, tx.Outs, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
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
	/* TODO add this back
	if err := tx.vm.putSubnets(db, subnets); err != nil {
		return nil, err
	}
	*/

	// Deduct tx fee from payer's account
	/* TODO: Deduct tx fee
	account, err := tx.vm.getAccount(db, tx.key.Address())
	if err != nil {
		return nil, err
	}
	account, err = account.Remove(0, tx.Nonce)
	if err != nil {
		return nil, err
	}
	if err := tx.vm.putAccount(db, account); err != nil {
		return nil, err
	}
	*/

	// Register new subnet in validator manager
	onAccept := func() {
		tx.vm.validators.PutValidatorSet(tx.id, validators.NewSet())
	}

	return onAccept, nil
}

// Bytes returns the byte representation of [tx]
func (tx *CreateSubnetTx) Bytes() []byte {
	if tx.bytes != nil {
		return tx.bytes
	}
	var err error
	tx.bytes, err = Codec.Marshal(tx)
	if err != nil {
		tx.vm.Ctx.Log.Error("problem marshaling tx: %v", err)
	}
	return tx.bytes
}

// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
/* TODO implement
func (vm *VM) newCreateSubnetTx(networkID uint32, nonce uint64, controlKeys []ids.ShortID,
	threshold uint16, payerKey *crypto.PrivateKeySECP256K1R,
) (*CreateSubnetTx, error) {
	tx := &CreateSubnetTx{UnsignedCreateSubnetTx: UnsignedCreateSubnetTx{
		NetworkID:   networkID,
		Nonce:       nonce,
		ControlKeys: controlKeys,
		Threshold:   threshold,
	}}

	if threshold == 0 && len(tx.ControlKeys) > 0 {
		return nil, errUnneededKeys
	}

	// Sort control keys
	ids.SortShortIDs(tx.ControlKeys)
	// Ensure control keys are unique
	if !ids.IsSortedAndUniqueShortIDs(tx.ControlKeys) {
		return nil, errControlKeysNotSortedAndUnique
	}

	unsignedIntf := interface{}(&tx.UnsignedCreateSubnetTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, err
	}

	sig, err := payerKey.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig)

	return tx, tx.initialize(vm)
}

// CreateSubnetTxList is a list of *CreateSubnetTx
type CreateSubnetTxList []*CreateSubnetTx

// Bytes returns the binary representation of [lst]
func (lst CreateSubnetTxList) Bytes() []byte {
	bytes, _ := Codec.Marshal(lst)
	return bytes
}
*/
