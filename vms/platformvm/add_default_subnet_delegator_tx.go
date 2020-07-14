// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	safemath "github.com/ava-labs/gecko/utils/math"
)

// UnsignedAddDefaultSubnetDelegatorTx is an unsigned addDefaultSubnetDelegatorTx
type UnsignedAddDefaultSubnetDelegatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the delegatee
	DurationValidator `serialize:"true"`
	// Where to send staked AVA after done validating
	Destination ids.ShortID `serialize:"true"`
}

// addDefaultSubnetDelegatorTx is a transaction that, if it is in a
// ProposalBlock that is accepted and followed by a Commit block, adds a
// delegator to the pending validator set of the default subnet. (That is, the
// validator in the tx will have their weight increase.)
type addDefaultSubnetDelegatorTx struct {
	UnsignedAddDefaultSubnetDelegatorTx `serialize:"true"`
	// Credentials that authorize the inputs to be spent
	Credentials []verify.Verifiable `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *addDefaultSubnetDelegatorTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *addDefaultSubnetDelegatorTx) initialize(vm *VM) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
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

// SyntacticVerify return nil iff [tx] is valid
func (tx *addDefaultSubnetDelegatorTx) SyntacticVerify() error {
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
	case tx.Wght < MinimumStakeAmount: // Ensure validator is staking at least the minimum amount
		return permError{errWeightTooSmall}
	}
	if err := tx.BaseTx.SyntacticVerify(); err != nil {
		return err
	}
	// Ensure staking length is not too short or long,
	// and that the inputs/outputs of this tx are syntactically valid
	stakingDuration := tx.Duration()
	switch {
	case stakingDuration < MinimumStakingDuration:
		return permError{errStakeTooShort}
	case stakingDuration > MaximumStakingDuration:
		return permError{errStakeTooLong}
	}
	if err := syntacticVerifySpend(tx, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return permError{err}
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetDelegatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Verify inputs/outputs and update the UTXO set
	if err := tx.vm.semanticVerifySpend(db, tx); err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't verify tx: %w", err)}
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, permError{err}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Ensure that the period this validator validates the specified subnet is a subnet of the time they validate the default subnet
	// First, see if they're currently validating the default subnet
	currentValidatorHeap, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	var dsValidator *addDefaultSubnetValidatorTx // default subnet validator
	if err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get current validators of default subnet: %v", err)}
	}
	if dsValidator, err = currentValidatorHeap.getDefaultSubnetStaker(tx.NodeID); err == nil {
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
		dsValidator, err = pendingDSValidators.getDefaultSubnetStaker(tx.NodeID)
		if err != nil {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	}

	pendingValidatorHeap, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	pendingValidatorHeap.Add(tx) // add validator to set of pending validators

	// If this proposal is committed, update the pending validator set to include the validator
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putPendingValidators(onCommitDB, pendingValidatorHeap, DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// If this proposal is aborted, return the AVAX (but not the tx fee)
	onAbortDB := versiondb.New(db)
	if err := tx.vm.putUTXO(onAbortDB, &ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        dsValidator.ID(), // Produced UTXO points to the default subnet validator tx
			OutputIndex: uint32(len(dsValidator.Outs())),
		},
		Asset: ava.Asset{ID: tx.vm.avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: tx.Validator.Wght, // Returned AVAX
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{tx.Destination}, // Spendable by destination address
			},
		},
	}); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	return onCommitDB, onAbortDB, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *addDefaultSubnetDelegatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

// Creates a new transaction
func (vm *VM) newAddDefaultSubnetDelegatorTx(
	stakeAmt, // Amount the delegator stakes
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they stop delegating
	nodeID ids.ShortID, // ID of the node we are delegating to
	destination ids.ShortID, // Address to returned staked tokens (and maybe reward) to
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens + fee
) (*addDefaultSubnetDelegatorTx, error) {

	// Calculate amount to be spent in this transaction
	toSpend, err := safemath.Add64(stakeAmt, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("overflow while calculating amount to spend")
	}

	// Calculate inputs, outputs, and keys used to sign this tx
	inputs, outputs, credKeys, err := vm.spend(vm.DB, toSpend, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the tx
	tx := &addDefaultSubnetDelegatorTx{
		UnsignedAddDefaultSubnetDelegatorTx: UnsignedAddDefaultSubnetDelegatorTx{
			BaseTx: BaseTx{
				NetworkID:    vm.Ctx.NetworkID,
				BlockchainID: ids.Empty,
				Inputs:       inputs,
				Outputs:      outputs,
			},
			DurationValidator: DurationValidator{
				Validator: Validator{
					NodeID: nodeID,
					Wght:   stakeAmt,
				},
				Start: startTime,
				End:   endTime,
			},
			Destination: destination,
		},
	}
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddDefaultSubnetDelegatorTx))
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedAddDefaultSubnetDelegatorTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// Attach credentials
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

	return tx, tx.initialize(vm)

}
