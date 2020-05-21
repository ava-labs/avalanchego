// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
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
	// NetworkID is the ID of the network this tx was issued on
	NetworkID uint32 `serialize:"true"`

	// Next unused nonce of account paying the transaction fee for this transaction.
	// Currently unused, as there are no tx fees.
	Nonce uint64 `serialize:"true"`

	// Each element in ControlKeys is the address of a public key
	// In order to add a validator to this subnet, a tx must be signed
	// with Threshold of these keys
	ControlKeys []ids.ShortID `serialize:"true"`
	Threshold   uint16        `serialize:"true"`
}

// CreateSubnetTx is a proposal to create a new subnet
type CreateSubnetTx struct {
	UnsignedCreateSubnetTx `serialize:"true"`

	// Signature on the UnsignedCreateSubnetTx's byte repr
	Sig [crypto.SECP256K1RSigLen]byte `serialize:"true"`

	// The public key that signed this transaction
	// The transaction fee will be paid from the corresponding account
	// (ie the account whose ID is [key].Address())
	// [key] is non-nil iff this tx is valid
	key crypto.PublicKey

	// The VM this tx exists within
	vm *VM

	// ID is this transaction's ID
	id ids.ID

	// Byte representation of this transaction (including signature)
	bytes []byte
}

// ID returns the ID of this transaction
func (tx *CreateSubnetTx) ID() ids.ID { return tx.id }

// SyntacticVerify nil iff [tx] is syntactically valid.
// If [tx] is valid, this method sets [tx.key]
func (tx *CreateSubnetTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.key != nil:
		return nil // Only verify the transaction once
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

	// Byte representation of the unsigned transaction
	unsignedIntf := interface{}(&tx.UnsignedCreateSubnetTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return err
	}

	// Recover signature from byte repr. of unsigned tx
	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:]) // the public key that signed [tx]
	if err != nil {
		return err
	}

	tx.key = key
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

	// Deduct tx fee from payer's account
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

// initialize sets [tx.vm] to [vm]
func (tx *CreateSubnetTx) initialize(vm *VM) error {
	tx.vm = vm
	txBytes, err := Codec.Marshal(tx) // byte repr. of the signed tx
	if err != nil {
		return err
	}
	tx.bytes = txBytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(txBytes))
	return nil
}

func (tx *CreateSubnetTx) Accept() error {
	if err := tx.vm.putTxStatus(tx.vm.DB, tx.ID(), choices.Accepted); err != nil {
		return err
	}
	tx.vm.DB.Commit()
	return nil
}

func (tx *CreateSubnetTx) Reject() error {
	if err := tx.vm.putTxStatus(tx.vm.DB, tx.ID(), choices.Rejected); err != nil {
		return err
	}
	tx.vm.DB.Commit()
	return nil
}

// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
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
