// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/hashing"
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
	vm *VM

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

// initialize [tx]
func (tx *addDefaultSubnetValidatorTx) initialize(vm *VM) error {
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
// TODO: Only do syntactic Verify once
func (tx *addDefaultSubnetValidatorTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return tempError{errNilTx}
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

	return nil
}

// SemanticVerify this transaction is valid.
// TODO make sure the ins and outs are semantically valid
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
		return nil, nil, nil, nil, permError{err}
	}

	// If this proposal is aborted, return the AVAX (but not the tx fee)
	onAbortDB := versiondb.New(db)
	return onCommitDB, onAbortDB, tx.vm.resetTimer, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *addDefaultSubnetValidatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

// NewAddDefaultSubnetValidatorTx returns a new NewAddDefaultSubnetValidatorTx
/* TODO: Implement
func (vm *VM) newAddDefaultSubnetValidatorTx(nonce, stakeAmt, startTime, endTime uint64, nodeID, destination ids.ShortID, shares, networkID uint32, key *crypto.PrivateKeySECP256K1R,
) (*addDefaultSubnetValidatorTx, error) {
	tx := &addDefaultSubnetValidatorTx{
		UnsignedAddDefaultSubnetValidatorTx: UnsignedAddDefaultSubnetValidatorTx{
			NetworkID: networkID,
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

	unsignedIntf := interface{}(&tx.UnsignedAddDefaultSubnetValidatorTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr. of unsigned tx
	if err != nil {
		return nil, err
	}

	sig, err := key.Sign(unsignedBytes) // Sign the transaction
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig) // have to do this because sig has type []byte but tx.Sig has type [65]byte

	return tx, tx.initialize(vm)
}
*/
