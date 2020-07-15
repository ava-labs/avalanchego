// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
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
	// IDs of control keys
	controlIDs []ids.ShortID
	// The validator
	SubnetValidator `serialize:"true"`
}

// addNonDefaultSubnetValidatorTx is a transaction that, if it is in a ProposeAddValidator block that
// is accepted and followed by a Commit block, adds a validator to the pending validator set of a subnet
// other than the default subnet.
// (That is, the validator in the tx will validate at some point in the future.)
// The transaction fee will be paid from the address whose ID is [Sigs[0].Address()]
type addNonDefaultSubnetValidatorTx struct {
	UnsignedAddNonDefaultSubnetValidatorTx `serialize:"true"`
	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
	// When a subnet is created, it specifies a set of public keys ("control keys") such
	// that in order to add a validator to the subnet, a tx must be signed with
	// a certain threshold of those keys
	// Each element of ControlSigs is the signature of one of those keys
	ControlSigs [][crypto.SECP256K1RSigLen]byte `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *addNonDefaultSubnetValidatorTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *addNonDefaultSubnetValidatorTx) initialize(vm *VM) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddNonDefaultSubnetValidatorTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddNonDefaultSubnetValidatorTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx) // byte representation of the signed transaction
	if err != nil {
		return fmt.Errorf("couldn't marshal addNonDefaultSubnetValidatorTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return nil
}

// SyntacticVerify return nil iff [tx] is valid
func (tx *addNonDefaultSubnetValidatorTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return tempError{errNilTx}
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.id.IsZero():
		return tempError{errInvalidID}
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return permError{errWrongNetworkID}
	case tx.NodeID.IsZero():
		return tempError{errInvalidID}
	case tx.Subnet.IsZero():
		return tempError{errInvalidID}
	case tx.Wght == 0: // Ensure the validator has some weight
		return permError{errWeightTooSmall}
	case !crypto.IsSortedAndUniqueSECP2561RSigs(tx.ControlSigs):
		return permError{errSigsNotUniqueOrNotSorted}
	}

	// Ensure staking length is not too short or long
	if stakingDuration := tx.Duration(); stakingDuration < MinimumStakingDuration {
		return permError{errStakeTooShort}
	} else if stakingDuration > MaximumStakingDuration {
		return permError{errStakeTooLong}
	}

	// Verify tx inputs and outputs are valid
	if err := syntacticVerifySpend(tx, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}

	// recover control signatures
	tx.controlIDs = make([]ids.ShortID, len(tx.ControlSigs))
	unsignedBytesHash := hashing.ComputeHash256(tx.unsignedBytes)
	for i, sig := range tx.ControlSigs {
		key, err := tx.vm.factory.RecoverHashPublicKey(unsignedBytesHash, sig[:])
		if err != nil {
			return permError{err}
		}
		tx.controlIDs[i] = key.Address()
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addNonDefaultSubnetValidatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil { // Ensure tx is syntactically valid
		return nil, nil, nil, nil, permError{err}
	} else if err := tx.vm.semanticVerifySpend(db, tx); err != nil { // Validate/update UTXOs
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't verify tx: %w", err)}
	}

	// Get info about the subnet we're adding a validator to
	subnets, err := tx.vm.getSubnets(db)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	var subnet *CreateSubnetTx
	for _, sn := range subnets {
		if sn.id.Equals(tx.SubnetID()) {
			subnet = sn
			break
		}
	}

	if subnet == nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("subnet %s does not exist", tx.SubnetID())}
	} else if len(tx.ControlSigs) != int(subnet.Threshold) {
		return nil, nil, nil, nil, permError{fmt.Errorf("expected tx to have %d control sigs but has %d", subnet.Threshold, len(tx.ControlSigs))}
	} else if !crypto.IsSortedAndUniqueSECP2561RSigs(tx.ControlSigs) {
		return nil, nil, nil, nil, permError{errors.New("control signatures aren't sorted")}
	}

	// Ensure the sigs on [tx] are valid
	controlKeys := ids.ShortSet{}
	controlKeys.Add(subnet.ControlKeys...)
	for _, controlID := range tx.controlIDs {
		if !controlKeys.Contains(controlID) {
			return nil, nil, nil, nil, permError{errors.New("tx has control signature from key not in subnet's ControlKeys")}
		}
	}

	// Ensure that the period this validator validates the specified subnet is a subnet of the time they validate the default subnet
	// First, see if they're currently validating the default subnet
	currentDSValidators, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get current validators of default subnet: %v", err)}
	}
	if dsValidator, err := currentDSValidators.getDefaultSubnetStaker(tx.NodeID); err == nil {
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil,
				permError{fmt.Errorf("time validating subnet [%v, %v] not subset of time validating default subnet [%v, %v]",
					tx.DurationValidator.StartTime(), tx.DurationValidator.EndTime(),
					dsValidator.StartTime(), dsValidator.EndTime())}
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

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get current timestamp: %v", err)}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) is at or after current chain timestamp (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentValidatorHeap, err := tx.vm.getCurrentValidators(db, tx.Subnet)
	if err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get current validators of subnet %s: %v", tx.Subnet, err)}
	}
	for _, currentVdr := range tx.vm.getValidators(currentValidatorHeap) {
		if currentVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the current validator set for subnet with ID %s",
				tx.NodeID,
				tx.Subnet)}
		}
	}

	// Ensure the proposed validator is not already slated to validate for the specified subnet
	pendingValidatorHeap, err := tx.vm.getPendingValidators(db, tx.Subnet)
	if err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get pending validators of subnet %s: %v", tx.Subnet, err)}
	}
	for _, pendingVdr := range tx.vm.getValidators(pendingValidatorHeap) {
		if pendingVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator with ID %s already in the pending validator set for subnet with ID %s",
				tx.NodeID,
				tx.Subnet)}
		}
	}
	pendingValidatorHeap.Add(tx) // add validator to set of pending validators

	// If this proposal is committed, update the pending validator set to include the validator
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putPendingValidators(onCommitDB, pendingValidatorHeap, tx.Subnet); err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't put current validators: %v", err)}
	}

	// If this proposal is aborted, chain state doesn't change
	onAbortDB := versiondb.New(db)
	return onCommitDB, onAbortDB, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *addNonDefaultSubnetValidatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

// Create a new transaction
func (vm *VM) newAddNonDefaultSubnetValidatorTx(
	weight, // Sampling weight of the new validator
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they top delegating
	nodeID ids.ShortID, // ID of the node validating
	subnetID ids.ID, // ID of the subnet the validator will validate
	controlKeys []*crypto.PrivateKeySECP256K1R, // Control keys for the subnet ID
	feeKeys []*crypto.PrivateKeySECP256K1R, // Pay the fee
) (*addNonDefaultSubnetValidatorTx, error) {

	// Get information about the subnet we're adding a chain to
	subnetInfo, err := vm.getSubnet(vm.DB, subnetID)
	if err != nil {
		return nil, fmt.Errorf("subnet %s doesn't exist", subnetID)
	}
	// Make sure we have enough of this subnet's control keys
	subnetControlKeys := ids.ShortSet{} // Subnet's
	for _, key := range subnetInfo.ControlKeys {
		subnetControlKeys.Add(key)
	}
	// [usableKeys] are the control keys that will sign this transaction
	usableKeys := make([]*crypto.PrivateKeySECP256K1R, 0, subnetInfo.Threshold)
	for _, key := range controlKeys {
		if subnetControlKeys.Contains(key.PublicKey().Address()) {
			usableKeys = append(usableKeys, key) // This key is useful
		}
		if len(usableKeys) == int(subnetInfo.Threshold) {
			break
		}
	}
	if len(usableKeys) != int(subnetInfo.Threshold) {
		return nil, fmt.Errorf("don't have enough control keys for subnet %s", subnetID)
	}

	// Calculate inputs, outputs, and keys used to sign this tx
	inputs, outputs, credKeys, err := vm.spend(vm.DB, vm.txFee, feeKeys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the tx
	tx := &addNonDefaultSubnetValidatorTx{
		UnsignedAddNonDefaultSubnetValidatorTx: UnsignedAddNonDefaultSubnetValidatorTx{
			BaseTx: BaseTx{
				NetworkID:    vm.Ctx.NetworkID,
				BlockchainID: ids.Empty,
				Inputs:       inputs,
				Outputs:      outputs,
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
		},
	}
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddNonDefaultSubnetValidatorTx))
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedAddNonDefaultSubnetValidatorTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// Attach credentials that allow UTXOs to be spent
	for _, inputKeys := range credKeys { // [inputKeys] are the keys used to authorize spend of an input
		cred := &secp256k1fx.Credential{}
		for _, key := range inputKeys {
			sig, err := key.SignHash(hash) // Sign hash(tx.unsignedBytes)
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.Credentials = append(tx.Credentials, cred) // Attach credntial to tx
	}
	// Attach control key signatures
	tx.ControlSigs = make([][crypto.SECP256K1RSigLen]byte, len(usableKeys))
	for i, key := range usableKeys {
		sig, err := key.SignHash(hash)
		if err != nil {
			return nil, err
		}
		// tx.ControlSigs[i] is type [65]byte but sig is type []byte do the below
		copy(tx.ControlSigs[i][:], sig)
	}
	crypto.SortSECP2561RSigs(tx.ControlSigs)

	return tx, tx.initialize(vm)
}
