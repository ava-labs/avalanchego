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
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
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
	vm *VM

	// ID of this tx
	id ids.ID

	// Byte representation of the unsigned tx
	unsignedBytes []byte

	// Byte representation of the signed transaction (ie with credentials)
	bytes []byte

	// Describes the validator
	DurationValidator `serialize:"true"`

	// ID of network this tx was issued on
	NetworkID uint32 `serialize:"true"`

	// Address to send staked AVA (and possibly reward) to when staker is done staking
	Destination ids.ShortID `serialize:"true"`

	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has Shares=300,000 then they take 30% of rewards from delegators
	Shares uint32 `serialize:"true"`

	// Input UTXOs
	Ins []*ava.TransferableInput `serialize:"true"`

	// Output UTXOs
	Outs []*ava.TransferableOutput `serialize:"true"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *UnsignedAddDefaultSubnetValidatorTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// addDefaultSubnetValidatorTx is a transaction that, if it is in a ProposeAddValidator block that
// is accepted and followed by a Commit block, adds a validator to the pending validator set of the default subnet.
// (That is, the validator in the tx will validate at some point in the future.)
type addDefaultSubnetValidatorTx struct {
	UnsignedAddDefaultSubnetValidatorTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Creds []verify.Verifiable `serialize:"true"`
}

// initialize [tx]
func (tx *addDefaultSubnetValidatorTx) initialize(vm *VM) error {
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddDefaultSubnetValidatorTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddDefaultSubnetValidatorTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx) // byte representation of the signed transaction
	if err != nil {
		return fmt.Errorf("couldn't marshal addDefaultSubnetValidatorTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return err
}

func (tx *addDefaultSubnetValidatorTx) ID() ids.ID { return tx.id }

// SyntacticVerify that this transaction is well formed
// If [tx] is valid, this method also populates [tx.accountID]
// TODO: Only do syntactic Verify once
func (tx *addDefaultSubnetValidatorTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return tempError{errNilTx}
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

	if err := syntacticVerifySpend(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetValidatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Update the UTXO set
	for _, in := range tx.Ins {
		utxoID := in.InputID() // ID of the UTXO that [in] spends
		if err := tx.vm.removeUTXO(db, utxoID); err != nil {
			return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't remove UTXO %s from UTXO set: %w", utxoID, err)}
		}
	}
	for _, out := range tx.Outs {
		if err := tx.vm.putUTXO(db, out); err != nil {
			return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't add UTXO %s to UTXO set: %w", err)}
		}
	}

	// Get the account that is paying the transaction fee and, if the proposal is to add a validator
	// to the default subnet, providing the staked $AVA.
	// The ID of this account is the address associated with the public key that signed this tx
	/*
		accountID := tx.senderID
		account, err := tx.vm.getAccount(db, accountID)
		if err != nil {
			return nil, nil, nil, nil, errDBAccount
		}

		// If the transaction adds a validator to the default subnet, also deduct
		// staked $AVA
		amount := tx.Weight()

		// The account if this block's proposal is committed and the validator is added
		// to the pending validator set. (Increase the account's nonce; decrease its balance.)
		newAccount, err := account.Remove(amount, tx.Nonce)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	*/

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

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentEvents, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	currentValidators := validators.NewSet()
	currentValidators.Set(tx.vm.getValidators(currentEvents))
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
	pendingValidators.Set(tx.vm.getValidators(pendingEvents))
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
