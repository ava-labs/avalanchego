// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errSigsNotUniqueOrNotSorted = errors.New("control signatures not unique or not sorted")
	errWrongNumberOfSignatures  = errors.New("wrong number of signatures")
	errDSValidatorSubset        = errors.New("all subnets must be a subset of the default subnet")

	_ UnsignedProposalTx = &UnsignedAddNonDefaultSubnetValidatorTx{}
	_ TimedTx            = &UnsignedAddNonDefaultSubnetValidatorTx{}
)

// UnsignedAddNonDefaultSubnetValidatorTx is an unsigned addNonDefaultSubnetValidatorTx
type UnsignedAddNonDefaultSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	Validator SubnetValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// StartTime of this validator
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) Verify(
	ctx *snow.Context,
	c codec.Codec,
	feeAmount uint64,
	feeAssetID ids.ID,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.SubnetAuth); err != nil {
		return err
	}

	// cache that this is valid
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) SemanticVerify(
	vm *VM,
	db database.Database,
	stx *Tx,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	// Verify the tx is well-formed
	if len(stx.Creds) == 0 {
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}
	if err := tx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current timestamp: %v", err)}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) is at or after current chain timestamp (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Ensure that the period this validator validates the specified subnet is a
	// subnet of the time they validate the default subnet. First, see if
	// they're currently validating the default subnet.
	currentDSValidators, err := vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current validators of default subnet: %v", err)}
	}
	if dsValidator, err := currentDSValidators.getDefaultSubnetStaker(tx.Validator.NodeID); err == nil {
		unsignedValidator := dsValidator.UnsignedTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.Validator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("time validating subnet [%v, %v] not subset of time validating default subnet [%v, %v]",
					tx.StartTime(), tx.EndTime(),
					unsignedValidator.StartTime(), unsignedValidator.EndTime())}
		}
	} else {
		// They aren't currently validating the default subnet. See if they will
		// validate the default subnet in the future.
		pendingDSValidators, err := vm.getPendingValidators(db, constants.DefaultSubnetID)
		if err != nil {
			return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get pending validators of default subnet: %v", err)}
		}
		dsValidator, err := pendingDSValidators.getDefaultSubnetStaker(tx.Validator.NodeID)
		if err != nil {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("validator would not be validating default subnet while validating non-default subnet")}
		}
		unsignedValidator := dsValidator.UnsignedTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.Validator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("time validating subnet [%v, %v] not subset of time validating default subnet [%v, %v]",
					tx.StartTime(), tx.EndTime(),
					unsignedValidator.StartTime(), unsignedValidator.EndTime())}
		}
	}

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentValidators, err := vm.getCurrentValidators(db, tx.Validator.Subnet)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current validators of subnet %s: %v",
			tx.Validator.Subnet, err)}
	}
	for _, currentVdr := range vm.getValidators(currentValidators) {
		if currentVdr.ID().Equals(tx.Validator.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the current validator set for subnet with ID %s",
				tx.Validator.NodeID,
				tx.Validator.Subnet)}
		}
	}

	// Ensure the proposed validator is not already slated to validate for the specified subnet
	pendingValidators, err := vm.getPendingValidators(db, tx.Validator.Subnet)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get pending validators of subnet %s: %v",
			tx.Validator.Subnet, err)}
	}
	for _, pendingVdr := range vm.getValidators(pendingValidators) {
		if pendingVdr.ID().Equals(tx.Validator.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the pending validator set for subnet with ID %s",
				tx.Validator.NodeID,
				tx.Validator.Subnet)}
		}
	}

	baseTxCredsLen := len(stx.Creds) - 1
	baseTxCreds := stx.Creds[:baseTxCredsLen]
	subnetCred := stx.Creds[baseTxCredsLen]

	subnet, txErr := vm.getSubnet(db, tx.Validator.Subnet)
	if err != nil {
		return nil, nil, nil, nil, txErr
	}
	unsignedSubnet := subnet.UnsignedTx.(*UnsignedCreateSubnetTx)
	if err := vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, unsignedSubnet.Owner); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(db, tx, tx.Ins, tx.Outs, baseTxCreds, vm.txFee, vm.Ctx.AVAXAssetID); err != nil {
		return nil, nil, nil, nil, err
	}

	txID := tx.ID()

	// Set up the DB if this tx is committed
	onCommitDB := versiondb.New(db)
	// Consume the UTXOS
	if err := vm.consumeInputs(onCommitDB, tx.Ins); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(onCommitDB, txID, tx.Outs); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Add the validator to the set of pending validators
	pendingValidators.Add(stx)
	// If this proposal is committed, update the pending validator set to include the delegator
	if err := vm.putPendingValidators(onCommitDB, pendingValidators, tx.Validator.Subnet); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	onAbortDB := versiondb.New(db)
	// Consume the UTXOS
	if err := vm.consumeInputs(onAbortDB, tx.Ins); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(onAbortDB, txID, tx.Outs); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	return onCommitDB, onAbortDB, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// Create a new transaction
func (vm *VM) newAddNonDefaultSubnetValidatorTx(
	weight, // Sampling weight of the new validator
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they top delegating
	nodeID ids.ShortID, // ID of the node validating
	subnetID ids.ID, // ID of the subnet the validator will validate
	keys []*crypto.PrivateKeySECP256K1R, // Keys to use for adding the validator
) (*Tx, error) {
	ins, outs, _, signers, err := vm.stake(vm.DB, keys, 0, vm.txFee)
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
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Validator: SubnetValidator{
			Validator: Validator{
				NodeID: nodeID,
				Start:  startTime,
				End:    endTime,
				Wght:   weight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID)
}
