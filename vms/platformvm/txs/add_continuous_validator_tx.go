// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"

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

	errTooRestakeShares = fmt.Errorf("a staker can only restake at most %d shares of its validation reward", reward.PercentDenominator)
)

type AddContinuousValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the validator
	Validator `serialize:"true" json:"validator"`
	// [Signer] is the BLS key for this validator.
	// Note: We do not enforce that the BLS key is unique across all validators.
	//       This means that validators can share a key if they so choose.
	//       However, a NodeID does uniquely map to a BLS key
	Signer signer.Signer `serialize:"true" json:"signer"`
	// Who is authorized to manage this validator
	ValidatorAuthKey fx.Owner `serialize:"true" json:"validatorAuthorizationKey"`
	// Where to send staked tokens when done validating
	StakeOuts []*avax.TransferableOutput `serialize:"true" json:"stake"`
	// Where to send validation rewards when done validating
	ValidatorRewardsOwner fx.Owner `serialize:"true" json:"validationRewardsOwner"`
	// how much of validation reward is restaked in next staking period,
	// along with previuosly staked amount
	ValidatorRewardRestakeShares uint32 `serialize:"true" json:"validationRewardsRestakeShares"`
	// Maximum amount of delegation weight that this validator permits.
	DelegationMaxWeight uint64 `serialize:"true" json:"delegationMaxWeight"`
	// Where to send delegation rewards when done validating
	DelegatorRewardsOwner fx.Owner `serialize:"true" json:"delegationRewardsOwner"`
	// Fee this validator charges delegators as a percentage, times 10,000
	// For example, if this validator has DelegationShares=300,000 then they
	// take 30% of rewards from delegators
	DelegationShares uint32 `serialize:"true" json:"Delegationshares"`
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
	tx.ValidatorAuthKey.InitCtx(ctx)
}

func (*AddContinuousValidatorTx) SubnetID() ids.ID {
	return constants.PlatformChainID
}

func (tx *AddContinuousValidatorTx) NodeID() ids.NodeID {
	return tx.Validator.NodeID
}

func (tx *AddContinuousValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	if err := tx.Signer.Verify(); err != nil {
		return nil, false, err
	}
	key := tx.Signer.Key()
	return key, key != nil, nil
}

func (tx *AddContinuousValidatorTx) ManagementKey() fx.Owner {
	return tx.ValidatorAuthKey
}

func (tx *AddContinuousValidatorTx) RestakeShares() uint32 {
	return tx.ValidatorRewardRestakeShares
}

func (*AddContinuousValidatorTx) CurrentPriority() Priority {
	return PrimaryNetworkContinuousValidatorCurrentPriority
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
	case tx.Validator.NodeID == ids.EmptyNodeID:
		return errEmptyNodeID
	case len(tx.StakeOuts) == 0: // Ensure there is provided stake
		return errNoStake
	case tx.ValidatorRewardRestakeShares > reward.PercentDenominator:
		return errTooRestakeShares
	case tx.DelegationShares > reward.PercentDenominator:
		return errTooManyDelegatorsShares
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := verify.All(
		&tx.Validator,
		tx.Signer,
		tx.ValidatorAuthKey,
		tx.ValidatorRewardsOwner,
		tx.DelegatorRewardsOwner,
	); err != nil {
		return fmt.Errorf("failed to verify validator, signer, rewards or staker owners: %w", err)
	}

	isPrimaryNetwork := true // tx.Subnet == constants.PrimaryNetworkID
	hasKey := tx.Signer.Key() != nil
	if hasKey != isPrimaryNetwork {
		return fmt.Errorf(
			"%w: hasKey=%v != isPrimaryNetwork=%v",
			errInvalidSigner,
			hasKey,
			isPrimaryNetwork,
		)
	}

	for _, out := range tx.StakeOuts {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("failed to verify output: %w", err)
		}
	}

	var (
		firstStakeOutput = tx.StakeOuts[0]
		stakedAssetID    = firstStakeOutput.AssetID()
		totalStakeWeight = firstStakeOutput.Output().Amount()
	)
	for _, out := range tx.StakeOuts[1:] {
		newWeight, err := math.Add64(totalStakeWeight, out.Output().Amount())
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
