// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
)

// UnsignedAddDefaultSubnetDelegatorTx is an unsigned addDefaultSubnetDelegatorTx
type UnsignedAddDefaultSubnetDelegatorTx struct {
	// Metadata, inputs and outputs
	BaseProposalTx `serialize:"true"`
	// Describes the delegatee
	DurationValidator `serialize:"true"`
	// Where to send staked tokens when done validating
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

// ID is this transaction's ID
func (tx *addDefaultSubnetDelegatorTx) ID() ids.ID {
	return tx.BaseProposalTx.BaseTx.ID()
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
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.id.IsZero():
		return errInvalidID
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return errWrongNetworkID
	case tx.NodeID.IsZero():
		return errInvalidID
	case tx.Wght < MinimumStakeAmount: // Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}
	if err := tx.BaseTx.SyntacticVerify(); err != nil {
		return err
	}
	// Ensure staking length is not too short or long,
	// and that the inputs/outputs of this tx are syntactically valid
	stakingDuration := tx.Duration()
	switch {
	case stakingDuration < MinimumStakingDuration:
		return errStakeTooShort
	case stakingDuration > MaximumStakingDuration:
		return errStakeTooLong
	}
	if err := syntacticVerifySpend(tx.OnCommitIns, tx.OnCommitOuts, tx.OnCommitCreds, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	} else if err := syntacticVerifySpend(tx.OnAbortIns, tx.OnAbortOuts, tx.OnAbortCreds, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetDelegatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	// Verify the tx is well-formed
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	// Verify inputs/outputs and update the UTXO set
	onCommitDB := versiondb.New(db)
	if err := tx.vm.semanticVerifySpend(onCommitDB, tx, tx.OnCommitIns, tx.OnCommitOuts, tx.OnCommitCreds); err != nil {
		return nil, nil, nil, nil, err
	}
	onAbortDB := versiondb.New(db)
	if err := tx.vm.semanticVerifySpend(onAbortDB, tx, tx.OnAbortIns, tx.OnAbortOuts, tx.OnAbortCreds); err != nil {
		return nil, nil, nil, nil, err
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Ensure that the period this validator validates the specified subnet is a subnet of the time they validate the default subnet
	// First, see if they're currently validating the default subnet
	currentValidatorHeap, err := tx.vm.getCurrentValidators(db, constants.DefaultSubnetID)
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
		pendingDSValidators, err := tx.vm.getPendingValidators(db, constants.DefaultSubnetID)
		if err != nil {
			return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get pending validators of default subnet: %v", err)}
		}
		dsValidator, err = pendingDSValidators.getDefaultSubnetStaker(tx.NodeID)
		if err != nil {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
		if !tx.DurationValidator.BoundedBy(dsValidator.StartTime(), dsValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	}

	// If this proposal is committed, update the pending validator set to include the validator
	pendingValidatorHeap, err := tx.vm.getPendingValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	pendingValidatorHeap.Add(tx) // add validator to set of pending validators
	if err := tx.vm.putPendingValidators(onCommitDB, pendingValidatorHeap, constants.DefaultSubnetID); err != nil {
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
	// Calculate inputs and outputs
	changeSpend := &spend{
		Threshold: 1,
		Locktime:  0,
		Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
	}
	// On commit, lock stakeAmt until staking ends
	onCommitSpends := []*spend{
		{
			Amount:   stakeAmt,
			Addrs:    []ids.ShortID{destination},
			Locktime: endTime,
		},
	}
	onCommitIns, onCommitOuts, onCommitCredKeys, err := vm.spend(vm.DB, keys, onCommitSpends, changeSpend, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// On abort, just pay tx fee
	onAbortIns, onAbortOuts, onAbortCredKeys, err := vm.spend(vm.DB, keys, nil, changeSpend, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	tx := &addDefaultSubnetDelegatorTx{
		UnsignedAddDefaultSubnetDelegatorTx: UnsignedAddDefaultSubnetDelegatorTx{
			BaseProposalTx: BaseProposalTx{
				BaseTx: &BaseTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					Ins:          onCommitIns,
					Outs:         onCommitOuts,
				},
				OnCommitOuts: onCommitOuts,
				OnAbortOuts:  onAbortOuts,
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
	tx.OnCommitCreds = make([]verify.Verifiable, len(onCommitCredKeys))
	for i, credKeys := range onCommitCredKeys {
		cred := &secp256k1fx.Credential{Sigs: make([][crypto.SECP256K1RSigLen]byte, len(credKeys))}
		for j, key := range credKeys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[j][:], sig)
		}
		tx.OnCommitCreds[i] = cred // Attach credential
	}
	tx.OnAbortCreds = make([]verify.Verifiable, len(onAbortCredKeys))
	for i, credKeys := range onAbortCredKeys {
		cred := &secp256k1fx.Credential{Sigs: make([][crypto.SECP256K1RSigLen]byte, len(credKeys))}
		for j, key := range credKeys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[j][:], sig)
		}
		tx.OnAbortCreds[i] = cred // Attach credential
	}
	return tx, tx.initialize(vm)

}
