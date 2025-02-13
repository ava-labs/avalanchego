// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/x/signer"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Wallet = (*walletWithOptions)(nil)

func NewWalletWithOptions(
	wallet Wallet,
	options ...common.Option,
) Wallet {
	return &walletWithOptions{
		wallet:  wallet,
		options: options,
	}
}

type walletWithOptions struct {
	wallet  Wallet
	options []common.Option
}

func (w *walletWithOptions) Builder() builder.Builder {
	return builder.NewWithOptions(
		w.wallet.Builder(),
		w.options...,
	)
}

func (w *walletWithOptions) Signer() signer.Signer {
	return w.wallet.Signer()
}

func (w *walletWithOptions) IssueBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueBaseTx(
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueCreateAssetTx(
		name,
		symbol,
		denomination,
		initialState,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTx(
	operations []*txs.Operation,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTx(
		operations,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxMintFT(
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxMintNFT(
		assetID,
		payload,
		owners,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxMintProperty(
		assetID,
		owner,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxBurnProperty(
	assetID ids.ID,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxBurnProperty(
		assetID,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueImportTx(
		chainID,
		to,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueExportTx(
		chainID,
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueUnsignedTx(
	utx txs.UnsignedTx,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueUnsignedTx(
		utx,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueTx(
	tx *txs.Tx,
	options ...common.Option,
) error {
	return w.wallet.IssueTx(
		tx,
		common.UnionOptions(w.options, options)...,
	)
}
