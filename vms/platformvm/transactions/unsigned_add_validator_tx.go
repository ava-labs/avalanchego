// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/entities"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const PercentDenominator = 1000000

var (
	errWeightTooLarge            = errors.New("weight of this validator is too large")
	errInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
	errTooManyShares             = fmt.Errorf("a staker can only require at most %d shares from delegators", PercentDenominator)
)

// UnsignedAddValidatorTx is an unsigned addValidatorTx
type UnsignedAddValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the delegatee
	Validator entities.Validator `serialize:"true" json:"validator"`
	// Where to send staked tokens when done validating
	Stake []*avax.TransferableOutput `serialize:"true" json:"stake"`
	// Where to send staking rewards when done validating
	RewardsOwner verify.Verifiable `serialize:"true" json:"rewardsOwner"`
	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has Shares=300,000 then they take 30% of rewards from delegators
	Shares uint32 `serialize:"true" json:"shares"`
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddValidatorTx) SyntacticVerify(synCtx ProposalTxSyntacticVerificationContext,
) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.Validator.Wght < synCtx.MinValidatorStake: // Ensure validator is staking at least the minimum amount
		return entities.ErrWeightTooSmall
	case tx.Validator.Wght > synCtx.MaxValidatorStake: // Ensure validator isn't staking too much
		return errWeightTooLarge
	case tx.Shares > PercentDenominator: // Ensure delegators shares are in the allowed amount
		return errTooManyShares
	case tx.Shares < synCtx.MinDelegationFee:
		return errInsufficientDelegationFee
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < synCtx.MinStakeDuration: // Ensure staking length is not too short
		return ErrStakeTooShort
	case duration > synCtx.MaxStakeDuration: // Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if err := tx.BaseTx.syntacticVerify(synCtx.Ctx, synCtx.C); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := verify.All(&tx.Validator, tx.RewardsOwner); err != nil {
		return fmt.Errorf("failed to verify validator or rewards owner: %w", err)
	}

	totalStakeWeight := uint64(0)
	for _, out := range tx.Stake {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("failed to verify output: %w", err)
		}
		newWeight, err := safemath.Add64(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight
	}

	switch {
	case !avax.IsSortedTransferableOutputs(tx.Stake, synCtx.C):
		return ErrOutputsNotSorted
	case totalStakeWeight != tx.Validator.Wght:
		return fmt.Errorf("validator weight %d is not equal to total stake weight %d", tx.Validator.Wght, totalStakeWeight)
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}
