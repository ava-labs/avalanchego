// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Wallet = &walletWithOptions{}

func NewWalletWithOptions(
	wallet Wallet,
	options ...common.Option,
) Wallet {
	return &walletWithOptions{
		Wallet:  wallet,
		options: options,
	}
}

type walletWithOptions struct {
	Wallet
	options []common.Option
}

func (w *walletWithOptions) Builder() Builder {
	return NewBuilderWithOptions(
		w.Wallet.Builder(),
		w.options...,
	)
}

func (w *walletWithOptions) IssueBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueBaseTx(
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueAddValidatorTx(
	validator *platformvm.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueAddValidatorTx(
		validator,
		rewardsOwner,
		shares,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueAddSubnetValidatorTx(
	validator *platformvm.SubnetValidator,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueAddSubnetValidatorTx(
		validator,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueAddDelegatorTx(
	validator *platformvm.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueAddDelegatorTx(
		validator,
		rewardsOwner,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueCreateChainTx(
		subnetID,
		genesis,
		vmID,
		fxIDs,
		chainName,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueCreateSubnetTx(
		owner,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueImportTx(
		sourceChainID,
		to,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueExportTx(
		chainID,
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueUnsignedTx(
	utx platformvm.UnsignedTx,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueUnsignedTx(
		utx,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueTx(
	tx *platformvm.Tx,
	options ...common.Option,
) (ids.ID, error) {
	return w.Wallet.IssueTx(
		tx,
		common.UnionOptions(w.options, options)...,
	)
}
