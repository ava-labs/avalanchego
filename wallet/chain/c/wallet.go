// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

var _ Wallet = (*wallet)(nil)

type Wallet interface {
	// Builder returns the builder that will be used to create the transactions.
	Builder() Builder

	// Signer returns the signer that will be used to sign the transactions.
	Signer() Signer

	// IssueImportTx creates, signs, and issues an import transaction that
	// attempts to consume all the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	IssueImportTx(
		chainID ids.ID,
		to ethcommon.Address,
		options ...common.Option,
	) (*evm.Tx, error)

	// IssueExportTx creates, signs, and issues an export transaction that
	// attempts to send all the provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	IssueExportTx(
		chainID ids.ID,
		outputs []*secp256k1fx.TransferOutput,
		options ...common.Option,
	) (*evm.Tx, error)

	// IssueUnsignedAtomicTx signs and issues the unsigned tx.
	IssueUnsignedAtomicTx(
		utx evm.UnsignedAtomicTx,
		options ...common.Option,
	) (*evm.Tx, error)

	// IssueAtomicTx issues the signed tx.
	IssueAtomicTx(
		tx *evm.Tx,
		options ...common.Option,
	) error
}

func NewWallet(
	builder Builder,
	signer Signer,
	avaxClient evm.Client,
	ethClient ethclient.Client,
	backend Backend,
) Wallet {
	return &wallet{
		Backend:    backend,
		builder:    builder,
		signer:     signer,
		avaxClient: avaxClient,
		ethClient:  ethClient,
	}
}

type wallet struct {
	Backend
	builder    Builder
	signer     Signer
	avaxClient evm.Client
	ethClient  ethclient.Client
}

func (w *wallet) Builder() Builder {
	return w.builder
}

func (w *wallet) Signer() Signer {
	return w.signer
}

func (w *wallet) IssueImportTx(
	chainID ids.ID,
	to ethcommon.Address,
	options ...common.Option,
) (*evm.Tx, error) {
	baseFee, err := w.baseFee(options)
	if err != nil {
		return nil, err
	}

	utx, err := w.builder.NewImportTx(chainID, to, baseFee, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedAtomicTx(utx, options...)
}

func (w *wallet) IssueExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*evm.Tx, error) {
	baseFee, err := w.baseFee(options)
	if err != nil {
		return nil, err
	}

	utx, err := w.builder.NewExportTx(chainID, outputs, baseFee, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedAtomicTx(utx, options...)
}

func (w *wallet) IssueUnsignedAtomicTx(
	utx evm.UnsignedAtomicTx,
	options ...common.Option,
) (*evm.Tx, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	tx, err := SignUnsignedAtomic(ctx, w.signer, utx)
	if err != nil {
		return nil, err
	}

	return tx, w.IssueAtomicTx(tx, options...)
}

func (w *wallet) IssueAtomicTx(
	tx *evm.Tx,
	options ...common.Option,
) error {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	txID, err := w.avaxClient.IssueTx(ctx, tx.SignedBytes())
	if err != nil {
		return err
	}

	if f := ops.PostIssuanceFunc(); f != nil {
		f(txID)
	}

	if ops.AssumeDecided() {
		return w.Backend.AcceptAtomicTx(ctx, tx)
	}

	if err := awaitTxAccepted(w.avaxClient, ctx, txID, ops.PollFrequency()); err != nil {
		return err
	}

	return w.Backend.AcceptAtomicTx(ctx, tx)
}

func (w *wallet) baseFee(options []common.Option) (*big.Int, error) {
	ops := common.NewOptions(options)
	baseFee := ops.BaseFee(nil)
	if baseFee != nil {
		return baseFee, nil
	}

	ctx := ops.Context()
	return w.ethClient.EstimateBaseFee(ctx)
}

// TODO: Upstream this function into coreth.
func awaitTxAccepted(
	c evm.Client,
	ctx context.Context,
	txID ids.ID,
	freq time.Duration,
	options ...rpc.Option,
) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		status, err := c.GetAtomicTxStatus(ctx, txID, options...)
		if err != nil {
			return err
		}

		if status == evm.Accepted {
			return nil
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
