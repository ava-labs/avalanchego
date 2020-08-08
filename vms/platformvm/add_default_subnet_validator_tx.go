// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
)

var (
	errNilTx          = errors.New("nil tx is invalid")
	errWrongNetworkID = errors.New("tx was issued with a different network ID")
	errWeightTooSmall = errors.New("weight of this validator is too low")
	errStakeTooShort  = errors.New("staking period is too short")
	errStakeTooLong   = errors.New("staking period is too long")
	errTooManyShares  = fmt.Errorf("a staker can only require at most %d shares from delegators", NumberOfShares)
)

// UnsignedAddDefaultSubnetValidatorTx is an unsigned addDefaultSubnetValidatorTx
type UnsignedAddDefaultSubnetValidatorTx struct {
	DurationValidator `serialize:"true"`
	NetworkID         uint32      `serialize:"true"`
	Nonce             uint64      `serialize:"true"`
	Destination       ids.ShortID `serialize:"true"`
	Shares            uint32      `serialize:"true"`
}

// addDefaultSubnetValidatorTx is a transaction that, if it is in a ProposeAddValidator block that
// is accepted and followed by a Commit block, adds a validator to the pending validator set of the default subnet.
// (That is, the validator in the tx will validate at some point in the future.)
type addDefaultSubnetValidatorTx struct {
	UnsignedAddDefaultSubnetValidatorTx `serialize:"true"`

	// Signature on the byte repr. of UnsignedAddValidatorTx
	Sig [crypto.SECP256K1RSigLen]byte `serialize:"true"`

	vm       *VM
	id       ids.ID
	senderID ids.ShortID

	// Byte representation of the signed transaction
	bytes []byte
}

// initialize [tx]
func (tx *addDefaultSubnetValidatorTx) initialize(vm *VM) error {
	tx.vm = vm
	bytes, err := Codec.Marshal(tx) // byte representation of the signed transaction
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	return err
}

func (tx *addDefaultSubnetValidatorTx) ID() ids.ID { return tx.id }

// SyntacticVerify that this transaction is well formed
// If [tx] is valid, this method also populates [tx.accountID]
func (tx *addDefaultSubnetValidatorTx) SyntacticVerify() TxError {
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
	case tx.Destination.IsZero():
		return tempError{errInvalidID}
	case tx.Wght < MinimumStakeAmount: // Ensure validator is staking at least the minimum amount
		return permError{errWeightTooSmall}
	case tx.Shares > NumberOfShares: // Ensure delegators shares are in the allowed amount
		return permError{errTooManyShares}
	}

	// Ensure staking length is not too short or long
	stakingDuration := tx.Duration()
	if stakingDuration < MinimumStakingDuration {
		return permError{errStakeTooShort}
	} else if stakingDuration > MaximumStakingDuration {
		return permError{errStakeTooLong}
	}

	// Byte representation of the unsigned transaction
	unsignedIntf := interface{}(&tx.UnsignedAddDefaultSubnetValidatorTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return permError{err}
	}

	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:]) // the public key that signed [tx]
	if err != nil {
		return permError{err}
	}
	tx.senderID = key.Address()

	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetValidatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, err
	}

	// Ensure the proposed validator starts after the current time
	currentTime, err := tx.vm.getTimestamp(db)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	startTime := tx.StartTime()
	if !currentTime.Before(startTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("chain timestamp (%s) not before validator's start time (%s)",
			currentTime,
			startTime)}
	}

	// Get the account that is paying the transaction fee and, if the proposal is to add a validator
	// to the default subnet, providing the staked $AVA.
	// The ID of this account is the address associated with the public key that signed this tx
	accountID := tx.senderID
	account, err := tx.vm.getAccount(db, accountID)
	if err != nil {
		return nil, nil, nil, nil, permError{errDBAccount}
	}

	// If the transaction adds a validator to the default subnet, also deduct
	// staked $AVA
	amount := tx.Weight()

	// The account if this block's proposal is committed and the validator is added
	// to the pending validator set. (Increase the account's nonce; decrease its balance.)
	newAccount, err := account.Remove(amount, tx.Nonce)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentEvents, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	currentValidators := validators.NewSet()
	if err := currentValidators.Set(tx.vm.getValidators(currentEvents)); err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("failed to initialize the new current validator set due to: %w",
			err)}
	}
	if currentValidators.Contains(tx.NodeID) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the current default validator set",
			tx.NodeID)}
	}

	// Ensure the proposed validator is not already slated to validate for the specified subnet
	pendingEvents, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	pendingValidators := validators.NewSet()
	if err := pendingValidators.Set(tx.vm.getValidators(pendingEvents)); err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("failed to initialize the new pending validator set due to: %w",
			err)}
	}
	if pendingValidators.Contains(tx.NodeID) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the pending default validator set",
			tx.NodeID)}
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

	return onCommitDB, onAbortDB, tx.vm.resetTimer, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *addDefaultSubnetValidatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

// NewAddDefaultSubnetValidatorTx returns a new NewAddDefaultSubnetValidatorTx
func (vm *VM) newAddDefaultSubnetValidatorTx(nonce, stakeAmt, startTime, endTime uint64, nodeID, destination ids.ShortID, shares, networkID uint32, key *crypto.PrivateKeySECP256K1R,
) (*addDefaultSubnetValidatorTx, error) {
	tx := &addDefaultSubnetValidatorTx{
		UnsignedAddDefaultSubnetValidatorTx: UnsignedAddDefaultSubnetValidatorTx{
			NetworkID: networkID,
			DurationValidator: DurationValidator{
				Validator: Validator{
					NodeID: nodeID,
					Wght:   stakeAmt,
				},
				Start: startTime,
				End:   endTime,
			},
			Nonce:       nonce,
			Destination: destination,
			Shares:      shares,
		},
	}

	unsignedIntf := interface{}(&tx.UnsignedAddDefaultSubnetValidatorTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr. of unsigned tx
	if err != nil {
		return nil, err
	}

	sig, err := key.Sign(unsignedBytes) // Sign the transaction
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig) // have to do this because sig has type []byte but tx.Sig has type [65]byte

	return tx, tx.initialize(vm)
}
