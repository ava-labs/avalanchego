// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	pChainValidator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var _ Builder = &builderWithOptions{}

type builderWithOptions struct {
	Builder
	options []common.Option
}

// NewBuilderWithOptions returns a new transaction builder that will use the
// given options by default.
//
// - [builder] is the builder that will be called to perform the underlying
//   opterations.
// - [options] will be provided to the builder in addition to the options
//   provided in the method calls.
func NewBuilderWithOptions(builder Builder, options ...common.Option) Builder {
	return &builderWithOptions{
		Builder: builder,
		options: options,
	}
}

func (b *builderWithOptions) GetBalance(
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	return b.Builder.GetBalance(
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) GetImportableBalance(
	chainID ids.ID,
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	return b.Builder.GetImportableBalance(
		chainID,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddValidatorTx(
	validator *pChainValidator.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*unsigned.AddValidatorTx, error) {
	return b.Builder.NewAddValidatorTx(
		validator,
		rewardsOwner,
		shares,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddSubnetValidatorTx(
	validator *pChainValidator.SubnetValidator,
	options ...common.Option,
) (*unsigned.AddSubnetValidatorTx, error) {
	return b.Builder.NewAddSubnetValidatorTx(
		validator,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewAddDelegatorTx(
	validator *pChainValidator.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*unsigned.AddDelegatorTx, error) {
	return b.Builder.NewAddDelegatorTx(
		validator,
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
) (*unsigned.CreateChainTx, error) {
	return b.Builder.NewCreateChainTx(
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
) (*unsigned.CreateSubnetTx, error) {
	return b.Builder.NewCreateSubnetTx(
		owner,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*unsigned.ImportTx, error) {
	return b.Builder.NewImportTx(
		sourceChainID,
		to,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*unsigned.ExportTx, error) {
	return b.Builder.NewExportTx(
		chainID,
		outputs,
		common.UnionOptions(b.options, options)...,
	)
}
