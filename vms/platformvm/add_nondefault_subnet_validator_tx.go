// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errSigsNotUniqueOrNotSorted = errors.New("control signatures not unique or not sorted")
	errWrongNumberOfSignatures  = errors.New("wrong number of signatures")
	errDSValidatorSubset        = errors.New("all subnets must be a subset of the default subnet")
)

// UnsignedAddNonDefaultSubnetValidatorTx is an unsigned addNonDefaultSubnetValidatorTx
type UnsignedAddNonDefaultSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	SubnetValidator `serialize:"true"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = vm.codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddNonDefaultSubnetValidatorTx: %w", err)
	}
	return nil
}

// SyntacticVerify return nil iff [tx] is valid
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(); err != nil {
		return err
	}

	stakingDuration := tx.Duration()
	switch {
	case tx.NodeID.IsZero(): // Ensure the validator has a valid ID
		return errInvalidID
	case tx.Subnet.IsZero(): // Ensure the subnet has a valid ID
		return errInvalidID
	case tx.Wght == 0: // Ensure the validator has some weight
		return errWeightTooSmall
	case stakingDuration < MinimumStakingDuration: // Ensure staking length is not too short
		return errStakeTooShort
	case stakingDuration > MaximumStakingDuration: // Ensure staking length is not too long
		return errStakeTooLong
	}

	if err := tx.SubnetAuth.Verify(); err != nil {
		return err
	} else if err := syntacticVerifySpend(tx.Ins, tx.Outs, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) SemanticVerify(
	db database.Database,
	creds []verify.Verifiable,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	// Verify the tx is well-formed
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	if len(creds) == 0 {
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current timestamp: %v", err)}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) is at or after current chain timestamp (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	subnet, err := tx.vm.getSubnet(db, tx.Subnet)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// TODO: check subnet ownership with the FX

	// Ensure that the period this validator validates the specified subnet is a
	// subnet of the time they validate the default subnet. First, see if
	// they're currently validating the default subnet.
	currentDSValidators, err := tx.vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current validators of default subnet: %v", err)}
	}
	if dsValidator, err := currentDSValidators.getDefaultSubnetStaker(tx.NodeID); err == nil {
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("time validating subnet [%v, %v] not subset of time validating default subnet [%v, %v]",
					tx.DurationValidator.StartTime(), tx.DurationValidator.EndTime(),
					dsValidator.StartTime(), dsValidator.EndTime())}
		}
	} else {
		// They aren't currently validating the default subnet. See if they will
		// validate the default subnet in the future.
		pendingDSValidators, err := tx.vm.getPendingValidators(db, constants.DefaultSubnetID)
		if err != nil {
			return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get pending validators of default subnet: %v", err)}
		}
		dsValidator, err := pendingDSValidators.getDefaultSubnetStaker(tx.NodeID)
		if err != nil {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("validator would not be validating default subnet while validating non-default subnet")}
		}
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("time validating subnet [%v, %v] not subset of time validating default subnet [%v, %v]",
					tx.DurationValidator.StartTime(), tx.DurationValidator.EndTime(),
					dsValidator.StartTime(), dsValidator.EndTime())}
		}
	}

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentValidators, err := tx.vm.getCurrentValidators(db, tx.Subnet)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current validators of subnet %s: %v", tx.Subnet, err)}
	}
	for _, currentVdr := range tx.vm.getValidators(currentValidators) {
		if currentVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the current validator set for subnet with ID %s",
				tx.NodeID,
				tx.Subnet)}
		}
	}

	// Ensure the proposed validator is not already slated to validate for the specified subnet
	pendingValidators, err := tx.vm.getPendingValidators(db, tx.Subnet)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get pending validators of subnet %s: %v", tx.Subnet, err)}
	}
	for _, pendingVdr := range tx.vm.getValidators(pendingValidators) {
		if pendingVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the pending validator set for subnet with ID %s",
				tx.NodeID,
				tx.Subnet)}
		}
	}

	baseTxCredsLen := len(creds) - 1
	baseTxCreds := creds[:baseTxCredsLen]
	subnetCred := creds[baseTxCredsLen]

	// Set up the DB if this tx is committed
	onCommitDB := versiondb.New(db)

	// Consume / produce the UTXOS
	if err := tx.vm.semanticVerifySpend(onCommitDB, tx, tx.Ins, tx.Outs, baseTxCreds); err != nil {
		return nil, nil, nil, nil, err
	}
	// Add the validator to the set of pending validators
	pendingValidators.Add(tx)
	// If this proposal is committed, update the pending validator set to include the delegator
	if err := tx.vm.putPendingValidators(onCommitDB, pendingValidators, tx.Subnet); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	onAbortDB := versiondb.New(db)

	// Consume / produce the UTXOS
	if err := tx.vm.semanticVerifySpend(onAbortDB, tx, tx.Ins, tx.Outs, baseTxCreds); err != nil {
		return nil, nil, nil, nil, err
	}

	return onCommitDB, onAbortDB, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

var (
	errUnknownOwner = errors.New("unknown owner type")
	errCantSign     = errors.New("can't sign the requested transaction")
)

// Create a new transaction
func (vm *VM) newAddNonDefaultSubnetValidatorTx(
	weight, // Sampling weight of the new validator
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they top delegating
	nodeID ids.ShortID, // ID of the node validating
	subnetID ids.ID, // ID of the subnet the validator will validate
	keys []*crypto.PrivateKeySECP256K1R, // Keys to use for adding the validator
) (*ProposalTx, error) {
	ins, outs, signers, err := vm.burn(vm.DB, keys, vm.txFee, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Get information about the subnet we're adding a chain to
	subnet, err := vm.getSubnet(vm.DB, subnetID)
	if err != nil {
		return nil, fmt.Errorf("subnet %s doesn't exist", subnetID)
	}

	// Make sure the owners of the subnet match the provided keys
	owner, ok := subnet.Owner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, errUnknownOwner
	}

	// Add the keys to a keychain
	kc := secp256k1fx.NewKeychain()
	for _, key := range keys {
		kc.Add(key)
	}

	// Attempt to prove ownership of the subnet
	now := uint64(vm.clock.Time().Unix())
	indices, subnetSigners, matches := kc.Match(owner, now)
	if !matches {
		return nil, errCantSign
	}
	signers = append(signers, subnetSigners)

	// Create the tx
	tx := &ProposalTx{UnsignedProposalTx: &UnsignedAddNonDefaultSubnetValidatorTx{
		BaseTx: BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		},
		SubnetValidator: SubnetValidator{
			DurationValidator: DurationValidator{
				Validator: Validator{
					NodeID: nodeID,
					Wght:   weight,
				},
				Start: startTime,
				End:   endTime,
			},
			Subnet: subnetID,
		},
		SubnetAuth: &secp256k1fx.Input{
			SigIndices: indices,
		},
	}}
	return tx, vm.signProposalTx(tx, signers)
}
