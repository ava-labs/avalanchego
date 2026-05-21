// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"github.com/MetalBlockchain/metalgo/graft/coreth/plugin/evm/atomic"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/vms/secp256k1fx"
	"github.com/MetalBlockchain/metalgo/wallet/subnet/primary/common"

	ethcommon "github.com/MetalBlockchain/libevm/common"
)

var _ Wallet = (*walletWithOptions)(nil)

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

func (w *walletWithOptions) IssueImportTx(
	chainID ids.ID,
	to ethcommon.Address,
	options ...common.Option,
) (*atomic.Tx, error) {
	return w.Wallet.IssueImportTx(
		chainID,
		to,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*atomic.Tx, error) {
	return w.Wallet.IssueExportTx(
		chainID,
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueUnsignedAtomicTx(
	utx atomic.UnsignedAtomicTx,
	options ...common.Option,
) (*atomic.Tx, error) {
	return w.Wallet.IssueUnsignedAtomicTx(
		utx,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueAtomicTx(
	tx *atomic.Tx,
	options ...common.Option,
) error {
	return w.Wallet.IssueAtomicTx(
		tx,
		common.UnionOptions(w.options, options)...,
	)
}
