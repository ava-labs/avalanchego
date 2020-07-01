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
)

var (
	errSigsNotSorted           = errors.New("control signatures not sorted")
	errWrongNumberOfSignatures = errors.New("wrong number of signatures")
	errDSValidatorSubset       = errors.New("all subnets must be a subset of the default subnet")
)

// UnsignedAddNonDefaultSubnetValidatorTx is an unsigned addNonDefaultSubnetValidatorTx
type UnsignedAddNonDefaultSubnetValidatorTx struct {
	vm *VM

	// Metadata, inputs and outputs
	CommonTx `serialize:"true"`

	// IDs of control keys
	controlIDs []ids.ShortID

	// The validator
	SubnetValidator `serialize:"true"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *UnsignedAddNonDefaultSubnetValidatorTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// addNonDefaultSubnetValidatorTx is a transaction that, if it is in a ProposeAddValidator block that
// is accepted and followed by a Commit block, adds a validator to the pending validator set of a subnet
// other than the default subnet.
// (That is, the validator in the tx will validate at some point in the future.)
// The transaction fee will be paid from the account whose ID is [Sigs[0].Address()]
type addNonDefaultSubnetValidatorTx struct {
	UnsignedAddNonDefaultSubnetValidatorTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Creds []verify.Verifiable `serialize:"true"`

	// When a subnet is created, it specifies a set of public keys ("control keys") such
	// that in order to add a validator to the subnet, a tx must be signed with
	// a certain threshold of those keys
	// Each element of ControlSigs is the signature of one of those keys
	ControlSigs [][crypto.SECP256K1RSigLen]byte `serialize:"true"`
}

// initialize [tx]
func (tx *addNonDefaultSubnetValidatorTx) initialize(vm *VM) error {
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddNonDefaultSubnetValidatorTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddNonDefaultSubnetValidatorTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx) // byte representation of the signed transaction
	if err != nil {
		return fmt.Errorf("couldn't marshal addNonDefaultSubnetValidatorTx: %w", err)
	}
	tx.vm = vm
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return nil
}

func (tx *addNonDefaultSubnetValidatorTx) ID() ids.ID { return tx.id }

// SyntacticVerify return nil iff [tx] is valid
// If [tx] is valid, sets [tx.accountID]
// TODO: only verify once
func (tx *addNonDefaultSubnetValidatorTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return tempError{errNilTx}
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
		return permError{errSigsNotSorted}
	}

	// Ensure staking length is not too short or long
	stakingDuration := tx.Duration()
	if stakingDuration < MinimumStakingDuration {
		return permError{errStakeTooShort}
	} else if stakingDuration > MaximumStakingDuration {
		return permError{errStakeTooLong}
	}

	// Byte representation of the unsigned transaction
	unsignedBytesHash := hashing.ComputeHash256(tx.unsignedBytes)

	tx.controlIDs = make([]ids.ShortID, len(tx.ControlSigs))
	// recover control signatures
	for i, sig := range tx.ControlSigs {
		key, err := tx.vm.factory.RecoverHashPublicKey(unsignedBytesHash, sig[:])
		if err != nil {
			return permError{err}
		}
		tx.controlIDs[i] = key.Address()
	}

	if err := syntacticVerifySpend(tx.Ins, tx.Outs, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	return nil
}

// getDefaultSubnetStaker ...
func (h *EventHeap) getDefaultSubnetStaker(id ids.ShortID) (*addDefaultSubnetValidatorTx, error) {
	for _, txIntf := range h.Txs {
		tx, ok := txIntf.(*addDefaultSubnetValidatorTx)
		if !ok {
			continue
		}

		if id.Equals(tx.NodeID) {
			return tx, nil
		}
	}
	return nil, errors.New("couldn't find validator in the default subnet")
}

// SemanticVerify this transaction is valid.
// TODO make sure the ins and outs are semantically valid
func (tx *addNonDefaultSubnetValidatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	// Ensure tx is syntactically valid
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, permError{err}
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
	}

	// Ensure the sigs on [tx] are valid
	if len(tx.ControlSigs) != int(subnet.Threshold) {
		return nil, nil, nil, nil, permError{fmt.Errorf("expected tx to have %d control sigs but has %d", subnet.Threshold, len(tx.ControlSigs))}
	}
	if !crypto.IsSortedAndUniqueSECP2561RSigs(tx.ControlSigs) {
		return nil, nil, nil, nil, permError{errors.New("control signatures aren't sorted")}
	}

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

	// Update the UTXO set
	for _, in := range tx.Ins {
		utxoID := in.InputID() // ID of the UTXO that [in] spends
		if err := tx.vm.removeUTXO(db, utxoID); err != nil {
			return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't remove UTXO %s from UTXO set: %w", utxoID, err)}
		}
	}
	for _, out := range tx.Outs {
		if err := tx.vm.putUTXO(db, tx.ID(), out); err != nil {
			return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't add UTXO to UTXO set: %w", err)}
		}
	}

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

/* TODO: Implement this
func (vm *VM) newAddNonDefaultSubnetValidatorTx(
	nonce,
	weight,
	startTime,
	endTime uint64,
	nodeID ids.ShortID,
	subnetID ids.ID,
	networkID uint32,
	controlKeys []*crypto.PrivateKeySECP256K1R,
	payerKey *crypto.PrivateKeySECP256K1R,
) (*addNonDefaultSubnetValidatorTx, error) {
	tx := &addNonDefaultSubnetValidatorTx{
		UnsignedAddNonDefaultSubnetValidatorTx: UnsignedAddNonDefaultSubnetValidatorTx{
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
			NetworkID: networkID,
		},
	}

	unsignedIntf := interface{}(&tx.UnsignedAddNonDefaultSubnetValidatorTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr. of unsigned tx
	if err != nil {
		return nil, err
	}
	unsignedHash := hashing.ComputeHash256(unsignedBytes)

	// Sign this tx with each control key
	tx.ControlSigs = make([][crypto.SECP256K1RSigLen]byte, len(controlKeys))
	for i, key := range controlKeys {
		sig, err := key.SignHash(unsignedHash)
		if err != nil {
			return nil, err
		}
		// tx.ControlSigs[i] is type [65]byte but sig is type []byte
		// so we have to do the below
		copy(tx.ControlSigs[i][:], sig)
	}
	crypto.SortSECP2561RSigs(tx.ControlSigs)

	// Sign this tx with the key of the tx fee payer
	sig, err := payerKey.SignHash(unsignedHash)
	if err != nil {
		return nil, err
	}
	copy(tx.PayerSig[:], sig)

	return tx, tx.initialize(vm)
}
*/
