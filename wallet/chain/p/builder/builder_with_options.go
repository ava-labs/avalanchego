// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Builder = (*builderWithOptions)(nil)

type builderWithOptions struct {
	builder Builder
	options []common.Option
}

// NewWithOptions returns a new builder that will use the given options by
// default.
//
//   - [builder] is the builder that will be called to perform the underlying
//     operations.
//   - [options] will be provided to the builder in addition to the options
//     provided in the method calls.
func NewWithOptions(builder Builder, options ...common.Option) Builder {
	return &builderWithOptions{
		builder: builder,
		options: options,
	}
}

func (b *builderWithOptions) Context() *Context {
	return b.builder.Context()
}

func (b *builderWithOptions) GetBalance(
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	return b.builder.GetBalance(
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) GetImportableBalance(
	chainID ids.ID,
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	return b.builder.GetImportableBalance(
		chainID,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.BaseTx, error) {
	return b.builder.NewBaseTx(
		outputs,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.AddValidatorTx, error) {
	return b.builder.NewAddValidatorTx(
		vdr,
		rewardsOwner,
		shares,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	options ...common.Option,
) (*txs.AddSubnetValidatorTx, error) {
	return b.builder.NewAddSubnetValidatorTx(
		vdr,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	options ...common.Option,
) (*txs.RemoveSubnetValidatorTx, error) {
	return b.builder.NewRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.AddDelegatorTx, error) {
	return b.builder.NewAddDelegatorTx(
		vdr,
		rewardsOwner,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...common.Option,
) (*txs.CreateChainTx, error) {
	return b.builder.NewCreateChainTx(
		subnetID,
		genesis,
		vmID,
		fxIDs,
		chainName,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.CreateSubnetTx, error) {
	return b.builder.NewCreateSubnetTx(
		owner,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.TransferSubnetOwnershipTx, error) {
	return b.builder.NewTransferSubnetOwnershipTx(
		subnetID,
		owner,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewConvertSubnetTx(
	subnetID ids.ID,
	chainID ids.ID,
	address []byte,
	options ...common.Option,
) (*txs.ConvertSubnetTx, error) {
	return b.builder.NewConvertSubnetTx(
		subnetID,
		chainID,
		address,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.ImportTx, error) {
	return b.builder.NewImportTx(
		sourceChainID,
		to,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.ExportTx, error) {
	return b.builder.NewExportTx(
		chainID,
		outputs,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewTransformSubnetTx(
	subnetID ids.ID,
	assetID ids.ID,
	initialSupply uint64,
	maxSupply uint64,
	minConsumptionRate uint64,
	maxConsumptionRate uint64,
	minValidatorStake uint64,
	maxValidatorStake uint64,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
	minDelegationFee uint32,
	minDelegatorStake uint64,
	maxValidatorWeightFactor byte,
	uptimeRequirement uint32,
	options ...common.Option,
) (*txs.TransformSubnetTx, error) {
	return b.builder.NewTransformSubnetTx(
		subnetID,
		assetID,
		initialSupply,
		maxSupply,
		minConsumptionRate,
		maxConsumptionRate,
		minValidatorStake,
		maxValidatorStake,
		minStakeDuration,
		maxStakeDuration,
		minDelegationFee,
		minDelegatorStake,
		maxValidatorWeightFactor,
		uptimeRequirement,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer signer.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.AddPermissionlessValidatorTx, error) {
	return b.builder.NewAddPermissionlessValidatorTx(
		vdr,
		signer,
		assetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		shares,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.AddPermissionlessDelegatorTx, error) {
	return b.builder.NewAddPermissionlessDelegatorTx(
		vdr,
		assetID,
		rewardsOwner,
		common.UnionOptions(b.options, options)...,
	)
}
