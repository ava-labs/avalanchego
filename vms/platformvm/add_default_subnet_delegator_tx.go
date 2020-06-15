// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
)

// UnsignedAddDefaultSubnetDelegatorTx is an unsigned addDefaultSubnetDelegatorTx
type UnsignedAddDefaultSubnetDelegatorTx struct {
	DurationValidator `serialize:"true"`
	NetworkID         uint32      `serialize:"true"`
	Nonce             uint64      `serialize:"true"`
	Destination       ids.ShortID `serialize:"true"`
}

// addDefaultSubnetDelegatorTx is a transaction that, if it is in a
// ProposalBlock that is accepted and followed by a Commit block, adds a
// delegator to the pending validator set of the default subnet. (That is, the
// validator in the tx will have their weight increase at some point in the
// future.) The transaction fee will be paid from the account who signed the
// transaction.
type addDefaultSubnetDelegatorTx struct {
	UnsignedAddDefaultSubnetDelegatorTx `serialize:"true"`

	// Sig is the signature of the public key whose corresponding account pays
	// the tx fee for this tx. ie the account with ID == [public key].Address()
	// pays the tx fee
	Sig [crypto.SECP256K1RSigLen]byte `serialize:"true"`

	vm       *VM
	id       ids.ID
	senderID ids.ShortID

	// Byte representation of the signed transaction
	bytes []byte
}

// initialize [tx]
func (tx *addDefaultSubnetDelegatorTx) initialize(vm *VM) error {
	tx.vm = vm
	bytes, err := Codec.Marshal(tx) // byte representation of the signed transaction
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	return err
}

func (tx *addDefaultSubnetDelegatorTx) ID() ids.ID { return tx.id }

// SyntacticVerify return nil iff [tx] is valid
// If [tx] is valid, sets [tx.accountID]
func (tx *addDefaultSubnetDelegatorTx) SyntacticVerify() TxError {
	switch {
	case tx == nil:
		return tempError{errNilTx}
	case !tx.senderID.IsZero():
		return nil // Only verify the transaction once
	case tx.id.IsZero():
		return tempError{errInvalidID}
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return permError{errWrongNetworkID}
	case tx.NodeID.IsZero():
		return tempError{errInvalidID}
	case tx.Wght < MinimumStakeAmount: // Ensure validator is staking at least the minimum amount
		return permError{errWeightTooSmall}
	}

	// Ensure staking length is not too short or long
	stakingDuration := tx.Duration()
	if stakingDuration < MinimumStakingDuration {
		return permError{errStakeTooShort}
	} else if stakingDuration > MaximumStakingDuration {
		return permError{errStakeTooLong}
	}

	unsignedIntf := interface{}(&tx.UnsignedAddDefaultSubnetDelegatorTx)
	// Byte representation of the unsigned transaction
	unsignedBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return permError{err}
	}

	// get account to pay tx fee from
	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:])
	if err != nil {
		return permError{err}
	}
	tx.senderID = key.Address()

	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetDelegatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, err
	}

	// Ensure the proposed validator starts after the current timestamp
	currentTimestamp, err := tx.vm.getTimestamp(db)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	validatorStartTime := tx.StartTime()
	if !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Get the account that is paying the transaction fee and, if the proposal
	// is to add a validator to the default subnet, providing the staked $AVA.
	// The ID of this account is the address associated with the public key that
	// signed this tx.
	accountID := tx.senderID
	account, err := tx.vm.getAccount(db, accountID)
	if err != nil {
		return nil, nil, nil, nil, permError{errDBAccount}
	}

	// The account if this block's proposal is committed and the validator is
	// added to the pending validator set. (Increase the account's nonce;
	// decrease its balance.)
	newAccount, err := account.Remove(0, tx.Nonce) // Remove also removes the fee
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure that the period this validator validates the specified subnet is a subnet of the time they validate the default subnet
	// First, see if they're currently validating the default subnet
	currentEvents, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get current validators of default subnet: %v", err)}
	}
	if dsValidator, err := currentEvents.getDefaultSubnetStaker(tx.NodeID); err == nil {
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	} else {
		// They aren't currently validating the default subnet.
		// See if they will validate the default subnet in the future.
		pendingDSValidators, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
		if err != nil {
			return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get pending validators of default subnet: %v", err)}
		}
		dsValidator, err := pendingDSValidators.getDefaultSubnetStaker(tx.NodeID)
		if err != nil {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	}

	pendingEvents, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	pendingEvents.Add(tx) // add validator to set of pending validators

	// If this proposal is committed, update the pending validator set to include the validator,
	// update the validator's account by removing the staked $AVA
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putPendingValidators(onCommitDB, pendingEvents, DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	if err := tx.vm.putAccount(onCommitDB, newAccount); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// If this proposal is aborted, chain state doesn't change
	onAbortDB := versiondb.New(db)

	return onCommitDB, onAbortDB, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *addDefaultSubnetDelegatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

func (vm *VM) newAddDefaultSubnetDelegatorTx(
	nonce,
	weight,
	startTime,
	endTime uint64,
	nodeID ids.ShortID,
	destination ids.ShortID,
	networkID uint32,
	key *crypto.PrivateKeySECP256K1R,
) (*addDefaultSubnetDelegatorTx, error) {
	tx := &addDefaultSubnetDelegatorTx{
		UnsignedAddDefaultSubnetDelegatorTx: UnsignedAddDefaultSubnetDelegatorTx{
			DurationValidator: DurationValidator{
				Validator: Validator{
					NodeID: nodeID,
					Wght:   weight,
				},
				Start: startTime,
				End:   endTime,
			},
			NetworkID:   networkID,
			Nonce:       nonce,
			Destination: destination,
		},
	}

	unsignedIntf := interface{}(&tx.UnsignedAddDefaultSubnetDelegatorTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr. of unsigned tx
	if err != nil {
		return nil, err
	}

	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig)

	return tx, tx.initialize(vm)
}
