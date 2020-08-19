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
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errNilTx          = errors.New("tx is nil")
	errWrongNetworkID = errors.New("tx was issued with a different network ID")
	errWeightTooSmall = errors.New("weight of this validator is too low")
	errStakeTooShort  = errors.New("staking period is too short")
	errStakeTooLong   = errors.New("staking period is too long")
	errTooManyShares  = fmt.Errorf("a staker can only require at most %d shares from delegators", NumberOfShares)

	_ UnsignedProposalTx = &UnsignedAddDefaultSubnetValidatorTx{}
	_ TimedTx            = &UnsignedAddDefaultSubnetValidatorTx{}
)

// UnsignedAddDefaultSubnetValidatorTx is an unsigned addDefaultSubnetValidatorTx
type UnsignedAddDefaultSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the delegatee
	Validator DurationValidator `serialize:"true" json:"validator"`
	// Where to send staked tokens when done validating
	Stake []*avax.TransferableOutput `serialize:"true" json:"stake"`
	// Where to send staking rewards when done validating
	RewardsOwner verify.Verifiable `serialize:"true" json:"rewardsOwner"`
	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has Shares=300,000 then they take 30% of rewards from delegators
	Shares uint32 `serialize:"true" json:"shares"`
}

// StartTime of this validator
func (tx *UnsignedAddDefaultSubnetValidatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx *UnsignedAddDefaultSubnetValidatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddDefaultSubnetValidatorTx) Verify(
	ctx *snow.Context,
	c codec.Codec,
	feeAmount uint64,
	feeAssetID ids.ID,
	minStake uint64,
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
	if err := verify.All(&tx.Validator, tx.RewardsOwner); err != nil {
		return err
	}

	totalStakeWeight := uint64(0)
	for _, out := range tx.Stake {
		if err := out.Verify(); err != nil {
			return err
		}
		newWeight, err := safemath.Add64(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight
	}

	switch {
	case !avax.IsSortedTransferableOutputs(tx.Stake, Codec):
		return errOutputsNotSorted
	case totalStakeWeight != tx.Validator.Wght:
		return errInvalidAmount
	case tx.Validator.Wght < minStake: // Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	case tx.Shares > NumberOfShares: // Ensure delegators shares are in the allowed amount
		return errTooManyShares
	}

	// cache that this is valid
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddDefaultSubnetValidatorTx) SemanticVerify(
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
	if err := tx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure the proposed validator starts after the current time
	if currentTime, err := vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if startTime := tx.StartTime(); !currentTime.Before(startTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("validator's start time (%s) at or after current timestamp (%s)",
			currentTime,
			startTime)}
	}

	// Ensure the proposed validator is not already a validator of the specified subnet
	currentValidators, err := vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	for _, currentVdr := range vm.getValidators(currentValidators) {
		if currentVdr.ID().Equals(tx.Validator.NodeID) {
			return nil, nil, nil, nil, permError{fmt.Errorf("validator %s already is already a Default Subnet validator",
				tx.Validator.NodeID)}
		}
	}

	// Ensure the proposed validator is not already slated to validate for the specified subnet
	pendingValidators, err := vm.getPendingValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	for _, pendingVdr := range vm.getValidators(pendingValidators) {
		if pendingVdr.ID().Equals(tx.Validator.NodeID) {
			return nil, nil, nil, nil, tempError{fmt.Errorf("validator %s is already a pending Default Subnet validator",
				tx.Validator.NodeID)}
		}
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(db, tx, tx.Ins, outs, stx.Creds, vm.txFee, vm.Ctx.AVAXAssetID); err != nil {
		return nil, nil, nil, nil, err
	}

	txID := tx.ID()

	// Verify inputs/outputs and update the UTXO set
	onCommitDB := versiondb.New(db)
	// Consume the UTXOS
	if err := vm.consumeInputs(onCommitDB, tx.Ins); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(onCommitDB, txID, tx.Outs); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// Add validator to set of pending validators
	pendingValidators.Add(stx)
	// If this proposal is committed, update the pending validator set to include the validator
	if err := vm.putPendingValidators(onCommitDB, pendingValidators, constants.DefaultSubnetID); err != nil {
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
func (tx *UnsignedAddDefaultSubnetValidatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// NewAddDefaultSubnetValidatorTx returns a new NewAddDefaultSubnetValidatorTx
func (vm *VM) newAddDefaultSubnetValidatorTx(
	stakeAmt, // Amount the delegator stakes
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they stop delegating
	nodeID ids.ShortID, // ID of the node we are delegating to
	rewardAddress ids.ShortID, // Address to send reward to, if applicable
	shares uint32, // 10,000 times percentage of reward taken from delegators
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens + fee
) (*Tx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := vm.stake(vm.DB, keys, stakeAmt, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := &UnsignedAddDefaultSubnetValidatorTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         unlockedOuts,
		}},
		Validator: DurationValidator{
			Validator: Validator{
				NodeID: nodeID,
				Wght:   stakeAmt,
			},
			Start: startTime,
			End:   endTime,
		},
		Stake: lockedOuts,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		Shares: shares,
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake)
}
