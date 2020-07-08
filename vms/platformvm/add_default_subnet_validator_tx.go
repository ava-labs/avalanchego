// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	safemath "github.com/ava-labs/gecko/utils/math"
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
	// Metadata, inputs and outputs
	CommonTx `serialize:"true"`
	// Describes the validator
	DurationValidator `serialize:"true"`
	// Address to send staked AVA (and possibly reward) to when staker is done staking
	Destination ids.ShortID `serialize:"true"`
	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has Shares=300,000 then they take 30% of rewards from delegators
	Shares uint32 `serialize:"true"`
}

// addDefaultSubnetValidatorTx is a transaction that, if it is in a ProposeAddValidator block that
// is accepted and followed by a Commit block, adds a validator to the pending validator set of the default subnet.
// (That is, the validator in the tx will validate at some point in the future.)
type addDefaultSubnetValidatorTx struct {
	UnsignedAddDefaultSubnetValidatorTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *addDefaultSubnetValidatorTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *addDefaultSubnetValidatorTx) initialize(vm *VM) error {
	if tx.vm != nil { // Already been initialized
		return nil
	}
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

// SyntacticVerify that this transaction is well formed
// If [tx] is valid, this method also populates [tx.accountID]
func (tx *addDefaultSubnetValidatorTx) SyntacticVerify() error {
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
	case tx.Destination.IsZero():
		return tempError{errInvalidID}
	case tx.Wght < MinimumStakeAmount: // Ensure validator is staking at least the minimum amount
		return permError{errWeightTooSmall}
	case tx.Shares > NumberOfShares: // Ensure delegators shares are in the allowed amount
		return permError{errTooManyShares}
	}

	// Ensure staking length is not too short or long,
	// and that the inputs/outputs of this tx are syntactically valid
	stakingDuration := tx.Duration()
	if stakingDuration < MinimumStakingDuration {
		return permError{errStakeTooShort}
	} else if stakingDuration > MaximumStakingDuration {
		return permError{errStakeTooLong}
	} else if err := syntacticVerifySpend(tx, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *addDefaultSubnetValidatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Verify inputs/outputs and update the UTXO set
	if err := tx.vm.semanticVerifySpend(db, tx); err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't verify tx: %w", err)}
	}

	// Ensure the proposed validator starts after the current time
	if currentTime, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, permError{err}
	} else if startTime := tx.StartTime(); !currentTime.Before(startTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) at or after current timestamp (%s)",
			currentTime,
			startTime)}
	}

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentValidatorHeap, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	for _, currentVdr := range tx.vm.getValidators(currentValidatorHeap) {
		if currentVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator %s already is already a Default Subnet validator",
				tx.NodeID)}
		}
	}

	// Ensure the proposed validator is not already slated to validate for the specified subnet
	pendingValidatorHeap, err := tx.vm.getPendingValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	for _, pendingVdr := range tx.vm.getValidators(pendingValidatorHeap) {
		if pendingVdr.ID().Equals(tx.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator %s is already a pending Default Subnet validator",
				tx.NodeID)}
		}
	}
	pendingValidatorHeap.Add(tx) // add validator to set of pending validators

	// If this proposal is committed, update the pending validator set to include the validator
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putPendingValidators(onCommitDB, pendingValidatorHeap, DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// If this proposal is aborted, return the AVAX (but not the tx fee)
	onAbortDB := versiondb.New(db)
	if err := tx.vm.putUTXO(onAbortDB, &ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        tx.ID(),            // Produced UTXO points to this transaction
			OutputIndex: virtualOutputIndex, // See [virtualOutputIndex comment]
		},
		Asset: ava.Asset{ID: tx.vm.avaxAssetID},
		Out: &ava.TransferableOutput{
			Asset: ava.Asset{ID: tx.vm.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:      tx.Validator.Wght, // Returned AVAX
				Locktime: 0,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{tx.Destination}, // Spendable by destination address
				},
			},
		},
	}); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	return onCommitDB, onAbortDB, tx.vm.resetTimer, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *addDefaultSubnetValidatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

// NewAddDefaultSubnetValidatorTx returns a new NewAddDefaultSubnetValidatorTx
func (vm *VM) newAddDefaultSubnetValidatorTx(
	stakeAmt uint64, // Amount being staked
	startTime uint64, // Unix time they start validating
	endTime uint64, // Unix time they stop validating
	nodeID ids.ShortID, // ID of node that will validate
	destination ids.ShortID, // Address to returned staked tokens (and maybe reward) to
	shares uint32, // 10,000 times percentage of reward taken from delegators
	keys []*crypto.PrivateKeySECP256K1R, // // Keys providing the staked tokens + fee
) (*addDefaultSubnetValidatorTx, error) {

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
	tx := &addDefaultSubnetValidatorTx{
		UnsignedAddDefaultSubnetValidatorTx: UnsignedAddDefaultSubnetValidatorTx{
			CommonTx: CommonTx{
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
			Shares:      shares,
		},
	}
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedAddDefaultSubnetValidatorTx))
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedAddDefaultSubnetValidatorTx: %w", err)
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
