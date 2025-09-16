// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ ValidatorTx      = (*AddContinuousValidatorTx)(nil)
	_ ContinuousStaker = (*AddContinuousValidatorTx)(nil)

	errMissingSigner = errors.New("missing signer")
	errMissingPeriod = errors.New("missing period")
)

type AddContinuousValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// Node ID of the validator
	ValidatorNodeID ids.NodeID `serialize:"true" json:"nodeID"`

	// Period (in seconds) of the staking cycle.
	Period uint64 `serialize:"true" json:"period"`

	// [Signer] is the BLS key for this validator.
	Signer signer.Signer `serialize:"true" json:"signer"`

	// Where to send staked tokens when done validating
	StakeOuts []*avax.TransferableOutput `serialize:"true" json:"stake"`

	// Where to send validation rewards when done validating
	ValidatorRewardsOwner fx.Owner `serialize:"true" json:"validationRewardsOwner"`

	// Where to send delegation rewards when done validating
	DelegatorRewardsOwner fx.Owner `serialize:"true" json:"delegationRewardsOwner"`

	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has DelegationShares=300,000 then they
	// take 30% of rewards from delegators
	DelegationShares uint32 `serialize:"true" json:"shares"`

	// Weight of this validator used when sampling
	Wght uint64 `serialize:"true" json:"weight"`
}

func (tx *AddContinuousValidatorTx) NodeID() ids.NodeID {
	return tx.ValidatorNodeID
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [AddContinuousValidatorTx]. Also sets the [ctx] to the given [vm.ctx] so
// that the addresses can be json marshalled into human readable format
func (tx *AddContinuousValidatorTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	for _, out := range tx.StakeOuts {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
	tx.ValidatorRewardsOwner.InitCtx(ctx)
	tx.DelegatorRewardsOwner.InitCtx(ctx)
}

func (tx *AddContinuousValidatorTx) SubnetID() ids.ID {
	return constants.PrimaryNetworkID
}

func (tx *AddContinuousValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	if err := tx.Signer.Verify(); err != nil {
		return nil, false, err
	}
	key := tx.Signer.Key()
	return key, key != nil, nil
}

func (tx *AddContinuousValidatorTx) PeriodDuration() time.Duration {
	return time.Duration(tx.Period) * time.Second
}

func (tx *AddContinuousValidatorTx) Weight() uint64 {
	return tx.Wght
}

func (tx *AddContinuousValidatorTx) PendingPriority() Priority {
	return PrimaryNetworkValidatorPendingPriority
}

func (tx *AddContinuousValidatorTx) CurrentPriority() Priority {
	return PrimaryNetworkValidatorCurrentPriority
}

func (tx *AddContinuousValidatorTx) Stake() []*avax.TransferableOutput {
	return tx.StakeOuts
}

func (tx *AddContinuousValidatorTx) ValidationRewardsOwner() fx.Owner {
	return tx.ValidatorRewardsOwner
}

func (tx *AddContinuousValidatorTx) DelegationRewardsOwner() fx.Owner {
	return tx.DelegatorRewardsOwner
}

func (tx *AddContinuousValidatorTx) Shares() uint32 {
	return tx.DelegationShares
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *AddContinuousValidatorTx) SyntacticVerify(ctx *snow.Context) error {
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
	case tx.Period == 0:
		return errMissingPeriod
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	if err := verify.All(tx.Signer, tx.ValidatorRewardsOwner, tx.DelegatorRewardsOwner); err != nil {
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

	firstStakeOutput := tx.StakeOuts[0]
	stakedAssetID := firstStakeOutput.AssetID()
	totalStakeWeight := firstStakeOutput.Output().Amount()
	for _, out := range tx.StakeOuts[1:] {
		newWeight, err := math.Add(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight

		assetID := out.AssetID()
		if assetID != stakedAssetID {
			return fmt.Errorf("%w: %q and %q", errMultipleStakedAssets, stakedAssetID, assetID)
		}
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

func (tx *AddContinuousValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddContinuousValidatorTx(tx)
}
