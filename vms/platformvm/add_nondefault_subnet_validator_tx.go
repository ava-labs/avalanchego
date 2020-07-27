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
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddNonDefaultSubnetValidatorTx: %w", err)
	}
	return nil
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) Verify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := verify.All(
		&tx.BaseTx,
		&tx.SubnetValidator,
		tx.SubnetAuth,
	); err != nil {
		return err
	}

	if err := syntacticVerifySpend(tx.Ins, tx.Outs, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) SemanticVerify(
	db database.Database,
	stx *ProposalTx,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	// Verify the tx is well-formed
	if len(stx.Credentials) == 0 {
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}
	if err := tx.Verify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current timestamp: %v", err)}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) is at or after current chain timestamp (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Ensure that the period this validator validates the specified subnet is a
	// subnet of the time they validate the default subnet. First, see if
	// they're currently validating the default subnet.
	currentDSValidators, err := tx.vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current validators of default subnet: %v", err)}
	}
	if dsValidator, err := currentDSValidators.getDefaultSubnetStaker(tx.NodeID); err == nil {
		unsignedValidator := dsValidator.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.DurationValidator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("time validating subnet [%v, %v] not subset of time validating default subnet [%v, %v]",
					tx.DurationValidator.StartTime(), tx.DurationValidator.EndTime(),
					unsignedValidator.StartTime(), unsignedValidator.EndTime())}
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
		unsignedValidator := dsValidator.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.DurationValidator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("time validating subnet [%v, %v] not subset of time validating default subnet [%v, %v]",
					tx.DurationValidator.StartTime(), tx.DurationValidator.EndTime(),
					unsignedValidator.StartTime(), unsignedValidator.EndTime())}
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

	baseTxCredsLen := len(stx.Credentials) - 1
	baseTxCreds := stx.Credentials[:baseTxCredsLen]
	subnetCred := stx.Credentials[baseTxCredsLen]

	subnet, txErr := tx.vm.getSubnet(db, tx.Subnet)
	if err != nil {
		return nil, nil, nil, nil, txErr
	}
	unsignedSubnet := subnet.UnsignedDecisionTx.(*UnsignedCreateSubnetTx)
	if err := tx.vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, unsignedSubnet.Owner); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Set up the DB if this tx is committed
	onCommitDB := versiondb.New(db)

	// Consume / produce the UTXOS
	if err := tx.vm.semanticVerifySpend(onCommitDB, tx, tx.Ins, tx.Outs, baseTxCreds); err != nil {
		return nil, nil, nil, nil, err
	}
	// Add the validator to the set of pending validators
	pendingValidators.Add(stx)
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

	subnetAuth, subnetSigners, err := vm.authorize(vm.DB, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Create the tx
	utx := &UnsignedAddNonDefaultSubnetValidatorTx{
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
		SubnetAuth: subnetAuth,
	}
	tx := &ProposalTx{UnsignedProposalTx: utx}
	if err := vm.signProposalTx(tx, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify()
}
