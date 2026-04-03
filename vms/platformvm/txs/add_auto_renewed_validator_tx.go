// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var _ ValidatorTx = (*AddAutoRenewedValidatorTx)(nil)

var (
	errMissingSigner                   = errors.New("missing signer")
	errMissingPeriod                   = errors.New("missing period")
	errTooManyAutoCompoundRewardShares = fmt.Errorf("a staker can only restake at most %d shares from rewards", reward.PercentDenominator)
	errInvalidStakedAsset              = errors.New("invalid staked asset")
)

type AddAutoRenewedValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// Node ID of the validator
	ValidatorNodeID ids.NodeID `serialize:"true" json:"nodeID"`

	// Signer is the BLS key for this validator.
	Signer signer.Signer `serialize:"true" json:"signer"`

	// Where to send staked tokens when done validating
	StakeOuts []*avax.TransferableOutput `serialize:"true" json:"stake"`

	// Where to send validation rewards when done validating
	ValidatorRewardsOwner fx.Owner `serialize:"true" json:"validationRewardsOwner"`

	// Where to send delegation rewards when done validating
	DelegatorRewardsOwner fx.Owner `serialize:"true" json:"delegationRewardsOwner"`

	// Who is authorized to modify the auto-restake config
	Owner fx.Owner `serialize:"true" json:"owner"`

	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has DelegationShares=300,000 then they
	// take 30% of rewards from delegators
	DelegationShares uint32 `serialize:"true" json:"shares"`

	// Weight of this validator used when sampling
	Wght uint64 `serialize:"true" json:"weight"`

	// Percentage of rewards to restake at the end of each cycle, expressed in millionths (percentage * 10,000).
	// Range [0..1_000_000]:
	//   0         = restake principal only; withdraw 100% of rewards
	//   300_000   = restake 30% of rewards; withdraw 70%
	//   1_000_000 = restake 100% of rewards; withdraw 0%
	AutoCompoundRewardShares uint32 `serialize:"true" json:"autoCompoundRewardShares"`

	// Period is the validation cycle duration, in seconds.
	Period uint64 `serialize:"true" json:"period"`
}

func (*AddAutoRenewedValidatorTx) SubnetID() ids.ID {
	return constants.PrimaryNetworkID
}

func (tx *AddAutoRenewedValidatorTx) NodeID() ids.NodeID {
	return tx.ValidatorNodeID
}

func (tx *AddAutoRenewedValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	if err := tx.Signer.Verify(); err != nil {
		return nil, false, err
	}
	key := tx.Signer.Key()
	return key, key != nil, nil
}

func (tx *AddAutoRenewedValidatorTx) Weight() uint64 {
	return tx.Wght
}

func (*AddAutoRenewedValidatorTx) CurrentPriority() Priority {
	return PrimaryNetworkValidatorCurrentPriority
}

func (tx *AddAutoRenewedValidatorTx) Stake() []*avax.TransferableOutput {
	return tx.StakeOuts
}

func (tx *AddAutoRenewedValidatorTx) ValidationRewardsOwner() fx.Owner {
	return tx.ValidatorRewardsOwner
}

func (tx *AddAutoRenewedValidatorTx) DelegationRewardsOwner() fx.Owner {
	return tx.DelegatorRewardsOwner
}

func (tx *AddAutoRenewedValidatorTx) Shares() uint32 {
	return tx.DelegationShares
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// AddAutoRenewedValidatorTx. Also sets the ctx to the given vm.ctx so
// that the addresses can be json marshalled into human readable format
func (tx *AddAutoRenewedValidatorTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	for _, out := range tx.StakeOuts {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
	tx.ValidatorRewardsOwner.InitCtx(ctx)
	tx.DelegatorRewardsOwner.InitCtx(ctx)
	tx.Owner.InitCtx(ctx)
}

// SyntacticVerify returns nil iff tx is valid
func (tx *AddAutoRenewedValidatorTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.ValidatorNodeID == ids.EmptyNodeID:
		return errEmptyNodeID
	case len(tx.StakeOuts) == 0:
		return errNoStake
	case tx.DelegationShares > reward.PercentDenominator:
		return errTooManyShares
	case tx.AutoCompoundRewardShares > reward.PercentDenominator:
		return errTooManyAutoCompoundRewardShares
	case tx.Period == 0:
		return errMissingPeriod
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	if err := verify.All(tx.Signer, tx.ValidatorRewardsOwner, tx.DelegatorRewardsOwner, tx.Owner); err != nil {
		return fmt.Errorf("failed to verify signer, or rewards owners: %w", err)
	}

	if tx.Signer.Key() == nil {
		return errMissingSigner
	}

	for _, out := range tx.StakeOuts {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("failed to verify output: %w", err)
		}
	}

	totalStakeWeight := uint64(0)
	for _, out := range tx.StakeOuts {
		if out.AssetID() != ctx.AVAXAssetID {
			return errInvalidStakedAsset
		}

		newWeight, err := safemath.Add(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight
	}

	switch {
	case !avax.IsSortedTransferableOutputs(tx.StakeOuts, Codec):
		return errOutputsNotSorted
	case totalStakeWeight != tx.Wght:
		return fmt.Errorf("%w: weight %d != stake %d", errValidatorWeightMismatch, tx.Wght, totalStakeWeight)
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *AddAutoRenewedValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddAutoRenewedValidatorTx(tx)
}
