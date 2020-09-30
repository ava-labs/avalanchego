// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errDSValidatorSubset = errors.New("all subnets' staking period must be a subset of the primary network")

	_ UnsignedProposalTx = &UnsignedAddSubnetValidatorTx{}
	_ TimedTx            = &UnsignedAddSubnetValidatorTx{}
)

// UnsignedAddSubnetValidatorTx is an unsigned addSubnetValidatorTx
type UnsignedAddSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	Validator SubnetValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// StartTime of this validator
func (tx *UnsignedAddSubnetValidatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx *UnsignedAddSubnetValidatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Weight of this validator
func (tx *UnsignedAddSubnetValidatorTx) Weight() uint64 {
	return tx.Validator.Weight()
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddSubnetValidatorTx) Verify(
	ctx *snow.Context,
	c codec.Codec,
	feeAmount uint64,
	feeAssetID ids.ID,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < minStakeDuration: // Ensure staking length is not too short
		return errStakeTooShort
	case duration > maxStakeDuration: // Ensure staking length is not too long
		return errStakeTooLong
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
func (tx *UnsignedAddSubnetValidatorTx) SemanticVerify(
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
	if err := tx.Verify(
		vm.Ctx,
		vm.codec,
		vm.txFee,
		vm.Ctx.AVAXAssetID,
		vm.minStakeDuration,
		vm.maxStakeDuration,
	); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get current timestamp: %v", err)}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) is at or after current chain timestamp (%s)",
			currentTimestamp,
			validatorStartTime)}
	} else if validatorStartTime.After(currentTimestamp.Add(maxFutureStartTime)) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator start time (%s) more than two weeks after current chain timestamp (%s)", validatorStartTime, currentTimestamp)}
	}

	// Ensure that the period this validator validates the specified subnet is a
	// subnet of the time they validate the primary network.
	vdr, isValidator, err := vm.isValidator(db, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	if isValidator && !tx.Validator.BoundedBy(vdr.StartTime(), vdr.EndTime()) {
		return nil, nil, nil, nil, permError{errDSValidatorSubset}
	}
	if !isValidator {
		// Ensure that the period this validator validates the specified subnet
		// is a subnet of the time they will validate the primary network.
		vdr, willBeValidator, err := vm.willBeValidator(db, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return nil, nil, nil, nil, tempError{err}
		}
		if !willBeValidator || !tx.Validator.BoundedBy(vdr.StartTime(), vdr.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	}

	// Ensure that the period this validator validates the specified subnet is a
	// subnet of the time they validate the primary network.
	_, isValidator, err = vm.isValidator(db, tx.Validator.Subnet, tx.Validator.NodeID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	if isValidator {
		return nil, nil, nil, nil, permError{fmt.Errorf("already validating subnet between")}
	}

	// Ensure that the period this validator validates the specified subnet
	// is a subnet of the time they will validate the primary network.
	_, willBeValidator, err := vm.willBeValidator(db, tx.Validator.Subnet, tx.Validator.NodeID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	if willBeValidator {
		return nil, nil, nil, nil, permError{fmt.Errorf("already validating subnet between")}
	}

	baseTxCredsLen := len(stx.Creds) - 1
	baseTxCreds := stx.Creds[:baseTxCredsLen]
	subnetCred := stx.Creds[baseTxCredsLen]

	subnet, timedErr := vm.getSubnet(db, tx.Validator.Subnet)
	if err != nil {
		return nil, nil, nil, nil, timedErr
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
	if err := vm.enqueueStaker(onCommitDB, tx.Validator.Subnet, stx); err != nil {
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
func (tx *UnsignedAddSubnetValidatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// Create a new transaction
func (vm *VM) newAddSubnetValidatorTx(
	weight, // Sampling weight of the new validator
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they top delegating
	nodeID ids.ShortID, // ID of the node validating
	subnetID ids.ID, // ID of the subnet the validator will validate
	keys []*crypto.PrivateKeySECP256K1R, // Keys to use for adding the validator
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*Tx, error) {
	ins, outs, _, signers, err := vm.stake(vm.DB, keys, 0, vm.txFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	subnetAuth, subnetSigners, err := vm.authorize(vm.DB, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Create the tx
	utx := &UnsignedAddSubnetValidatorTx{
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
	return tx, utx.Verify(
		vm.Ctx,
		vm.codec,
		vm.txFee,
		vm.Ctx.AVAXAssetID,
		vm.minStakeDuration,
		vm.maxStakeDuration,
	)
}
