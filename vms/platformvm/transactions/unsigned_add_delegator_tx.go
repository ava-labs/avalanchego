// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/entities"
)

var (
	ErrStakeTooShort = errors.New("staking period is too short")
	ErrStakeTooLong  = errors.New("staking period is too long")
)

// UnsignedAddDelegatorTx is an unsigned addDelegatorTx
type UnsignedAddDelegatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the delegatee
	Validator entities.Validator `serialize:"true" json:"validator"`
	// Where to send staked tokens when done validating
	Stake []*avax.TransferableOutput `serialize:"true" json:"stake"`
	// Where to send staking rewards when done validating
	RewardsOwner verify.Verifiable `serialize:"true" json:"rewardsOwner"`
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddDelegatorTx) SyntacticVerify(synCtx ProposalTxSyntacticVerificationContext,
) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < synCtx.MinStakeDuration: // Ensure staking length is not too short
		return ErrStakeTooShort
	case duration > synCtx.MaxStakeDuration: // Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if err := tx.BaseTx.syntacticVerify(synCtx.Ctx, synCtx.C); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.RewardsOwner); err != nil {
		return fmt.Errorf("failed to verify validator or rewards owner: %w", err)
	}

	totalStakeWeight := uint64(0)
	for _, out := range tx.Stake {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output verification failed: %w", err)
		}
		newWeight, err := math.Add64(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight
	}

	switch {
	case !avax.IsSortedTransferableOutputs(tx.Stake, synCtx.C):
		return ErrOutputsNotSorted
	case totalStakeWeight != tx.Validator.Wght:
		return fmt.Errorf("delegator weight %d is not equal to total stake weight %d", tx.Validator.Wght, totalStakeWeight)
	case tx.Validator.Wght < synCtx.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return entities.ErrWeightTooSmall
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}
