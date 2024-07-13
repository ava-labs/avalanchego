// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Builder = (*builderWithOptions)(nil)

type builderWithOptions struct {
	builder Builder
	options []common.Option
}

// NewWithOptions returns a new transaction builder that will use the given
// options by default.
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

func (b *builderWithOptions) GetFTBalance(
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	return b.builder.GetFTBalance(
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
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.BaseTx, error) {
	return b.builder.NewBaseTx(
		outputs,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.CreateAssetTx, error) {
	return b.builder.NewCreateAssetTx(
		name,
		symbol,
		denomination,
		initialState,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTx(
	operations []*txs.Operation,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTx(
		operations,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxMintFT(
		outputs,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxMintNFT(
		assetID,
		payload,
		owners,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxMintProperty(
		assetID,
		owner,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxBurnProperty(
	assetID ids.ID,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxBurnProperty(
		assetID,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.ImportTx, error) {
	return b.builder.NewImportTx(
		chainID,
		to,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	feeCalc fee.Calculator,
	options ...common.Option,
) (*txs.ExportTx, error) {
	return b.builder.NewExportTx(
		chainID,
		outputs,
		feeCalc,
		common.UnionOptions(b.options, options)...,
	)
}
