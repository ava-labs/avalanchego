// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
)

// UnsignedAddDefaultSubnetDelegatorTx is an unsigned addDefaultSubnetDelegatorTx
type UnsignedAddDefaultSubnetDelegatorTx struct {
	vm *VM

	// ID of this tx
	id ids.ID

	// Byte representation of this unsigned tx
	unsignedBytes []byte

	// Byte representation of the signed transaction (ie with credentials)
	bytes []byte

	// Describes the delegatee
	DurationValidator `serialize:"true"`

	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true"`

	// Where to send staked AVA after done validating
	Destination ids.ShortID `serialize:"true"`

	// Input UTXOs
	Ins []*ava.TransferableInput `serialize:"true"`

	// Output UTXOs
	Outs []*ava.TransferableOutput `serialize:"true"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *UnsignedAddDefaultSubnetDelegatorTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// addDefaultSubnetDelegatorTx is a transaction that, if it is in a
// ProposalBlock that is accepted and followed by a Commit block, adds a
// delegator to the pending validator set of the default subnet. (That is, the
// validator in the tx will have their weight increase at some point in the
// future.) The transaction fee will be paid from the account who signed the
// transaction.
type addDefaultSubnetDelegatorTx struct {
	UnsignedAddDefaultSubnetDelegatorTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Creds []verify.Verifiable `serialize:"true"`
}

// initialize [tx]
func (tx *addDefaultSubnetDelegatorTx) initialize(vm *VM) error {
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddDefaultSubnetDelegatorTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddDefaultSubnetDelegatorTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx) // byte representation of the signed transaction
	if err != nil {
		return fmt.Errorf("couldn't marshal addDefaultSubnetDelegatorTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return nil
}

func (tx *addDefaultSubnetDelegatorTx) ID() ids.ID { return tx.id }

// SyntacticVerify return nil iff [tx] is valid
// If [tx] is valid, sets [tx.accountID]
// TODO: Only do syntactic Verify once
func (tx *addDefaultSubnetDelegatorTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.id.IsZero():
		return errInvalidID
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return errWrongNetworkID
	case tx.NodeID.IsZero():
		return errInvalidID
	case tx.Wght < MinimumStakeAmount: // Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}

	// Ensure staking length is not too short or long
	stakingDuration := tx.Duration()
	if stakingDuration < MinimumStakingDuration {
		return errStakeTooShort
	} else if stakingDuration > MaximumStakingDuration {
		return errStakeTooLong
	}

	if err := syntacticVerifySpend(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetDelegatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), error) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, err
	}

	// Update the UTXO set
	for _, in := range tx.Ins {
		utxoID := in.InputID() // ID of the UTXO that [in] spends
		if err := tx.vm.removeUTXO(db, utxoID); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("couldn't remove UTXO %s from UTXO set: %w", utxoID, err)
		}
	}
	for _, out := range tx.Outs {
		if err := tx.vm.putUTXO(db, out); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("couldn't add UTXO %s to UTXO set: %w", err)
		}
	}

	// Ensure the proposed validator starts after the current timestamp
	currentTimestamp, err := tx.vm.getTimestamp(db)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	validatorStartTime := tx.StartTime()
	if !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, fmt.Errorf("chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			validatorStartTime)
	}

	// Ensure that the period this validator validates the specified subnet is a subnet of the time they validate the default subnet
	// First, see if they're currently validating the default subnet
	currentEvents, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("couldn't get current validators of default subnet: %v", err)
	}
	if dsValidator, err := currentEvents.getDefaultSubnetStaker(tx.NodeID); err == nil {
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil, errDSValidatorSubset
		}
	} else {
		// They aren't currently validating the default subnet.
		// See if they will validate the default subnet in the future.
		pendingDSValidators, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("couldn't get pending validators of default subnet: %v", err)
		}
		dsValidator, err := pendingDSValidators.getDefaultSubnetStaker(tx.NodeID)
		if err != nil {
			return nil, nil, nil, nil, errDSValidatorSubset
		}
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil, errDSValidatorSubset
		}
	}

	pendingEvents, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	pendingEvents.Add(tx) // add validator to set of pending validators

	// If this proposal is committed, update the pending validator set to include the validator,
	// update the validator's account by removing the staked $AVA
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putPendingValidators(onCommitDB, pendingEvents, DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := tx.vm.putAccount(onCommitDB, newAccount); err != nil {
		return nil, nil, nil, nil, err
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

// TODO: Implement
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
	// Get UTXOs of sender
	addr := key.PublicKey().Address()

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
