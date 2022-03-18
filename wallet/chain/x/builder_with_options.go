// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
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

func (b *builderWithOptions) GetFTBalance(
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	return b.Builder.GetFTBalance(
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

func (b *builderWithOptions) NewBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*avm.BaseTx, error) {
	return b.Builder.NewBaseTx(
		outputs,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	options ...common.Option,
) (*avm.CreateAssetTx, error) {
	return b.Builder.NewCreateAssetTx(
		name,
		symbol,
		denomination,
		initialState,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTx(
	operations []*avm.Operation,
	options ...common.Option,
) (*avm.OperationTx, error) {
	return b.Builder.NewOperationTx(
		operations,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*avm.OperationTx, error) {
	return b.Builder.NewOperationTxMintFT(
		outputs,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	options ...common.Option,
) (*avm.OperationTx, error) {
	return b.Builder.NewOperationTxMintNFT(
		assetID,
		payload,
		owners,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*avm.OperationTx, error) {
	return b.Builder.NewOperationTxMintProperty(
		assetID,
		owner,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxBurnProperty(
	assetID ids.ID,
	options ...common.Option,
) (*avm.OperationTx, error) {
	return b.Builder.NewOperationTxBurnProperty(
		assetID,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*avm.ImportTx, error) {
	return b.Builder.NewImportTx(
		chainID,
		to,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*avm.ExportTx, error) {
	return b.Builder.NewExportTx(
		chainID,
		outputs,
		common.UnionOptions(b.options, options)...,
	)
}
