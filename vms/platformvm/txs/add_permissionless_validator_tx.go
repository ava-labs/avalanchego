// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
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
	_ ValidatorTx     = (*AddPermissionlessValidatorTx)(nil)
	_ ScheduledStaker = (*AddPermissionlessDelegatorTx)(nil)

	errEmptyNodeID             = errors.New("validator nodeID cannot be empty")
	errNoStake                 = errors.New("no stake")
	errInvalidSigner           = errors.New("invalid signer")
	errMultipleStakedAssets    = errors.New("multiple staked assets")
	errValidatorWeightMismatch = errors.New("validator weight mismatch")
)

// AddPermissionlessValidatorTx is an unsigned addPermissionlessValidatorTx
type AddPermissionlessValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the validator
	Validator `serialize:"true" json:"validator"`
	// ID of the subnet this validator is validating
	Subnet ids.ID `serialize:"true" json:"subnetID"`
	// If the [Subnet] is the primary network, [Signer] is the BLS key for this
	// validator. If the [Subnet] is not the primary network, this value is the
	// empty signer
	// Note: We do not enforce that the BLS key is unique across all validators.
	//       This means that validators can share a key if they so choose.
	//       However, a NodeID does uniquely map to a BLS key
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
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [AddPermissionlessValidatorTx]. Also sets the [ctx] to the given [vm.ctx] so
// that the addresses can be json marshalled into human readable format
func (tx *AddPermissionlessValidatorTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	for _, out := range tx.StakeOuts {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
	tx.ValidatorRewardsOwner.InitCtx(ctx)
	tx.DelegatorRewardsOwner.InitCtx(ctx)
}

func (tx *AddPermissionlessValidatorTx) SubnetID() ids.ID {
	return tx.Subnet
}

func (tx *AddPermissionlessValidatorTx) NodeID() ids.NodeID {
	return tx.Validator.NodeID
}

func (tx *AddPermissionlessValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	if err := tx.Signer.Verify(); err != nil {
		return nil, false, err
	}
	key := tx.Signer.Key()
	return key, key != nil, nil
}

func (tx *AddPermissionlessValidatorTx) PendingPriority() Priority {
	if tx.Subnet == constants.PrimaryNetworkID {
		return PrimaryNetworkValidatorPendingPriority
	}
	return SubnetPermissionlessValidatorPendingPriority
}

func (tx *AddPermissionlessValidatorTx) CurrentPriority() Priority {
	if tx.Subnet == constants.PrimaryNetworkID {
		return PrimaryNetworkValidatorCurrentPriority
	}
	return SubnetPermissionlessValidatorCurrentPriority
}

func (tx *AddPermissionlessValidatorTx) Stake() []*avax.TransferableOutput {
	return tx.StakeOuts
}

func (tx *AddPermissionlessValidatorTx) ValidationRewardsOwner() fx.Owner {
	return tx.ValidatorRewardsOwner
}

func (tx *AddPermissionlessValidatorTx) DelegationRewardsOwner() fx.Owner {
	return tx.DelegatorRewardsOwner
}

func (tx *AddPermissionlessValidatorTx) Shares() uint32 {
	return tx.DelegationShares
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *AddPermissionlessValidatorTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.Validator.NodeID == ids.EmptyNodeID:
		return errEmptyNodeID
	case len(tx.StakeOuts) == 0: // Ensure there is provided stake
		return errNoStake
	case tx.DelegationShares > reward.PercentDenominator:
		return errTooManyShares
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := verify.All(&tx.Validator, tx.Signer, tx.ValidatorRewardsOwner, tx.DelegatorRewardsOwner); err != nil {
		return fmt.Errorf("failed to verify validator, signer, or rewards owners: %w", err)
	}

	hasKey := tx.Signer.Key() != nil
	isPrimaryNetwork := tx.Subnet == constants.PrimaryNetworkID
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

func (tx *AddPermissionlessValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddPermissionlessValidatorTx(tx)
}
