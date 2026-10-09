// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// TODO: Add unit tests for the C-Chain wallet, builder, signer, and backend.

package c

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/client"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ethcommon "github.com/ava-labs/libevm/common"
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
	) (*tx.Tx, error)

	// IssueExportTx creates, signs, and issues an export transaction that
	// attempts to send all the provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	IssueExportTx(
		chainID ids.ID,
		outputs []*secp256k1fx.TransferOutput,
		options ...common.Option,
	) (*tx.Tx, error)

	// IssueUnsignedTx signs and issues the unsigned tx.
	IssueUnsignedTx(
		utx tx.Unsigned,
		options ...common.Option,
	) (*tx.Tx, error)

	// IssueTx issues the signed tx.
	IssueTx(
		t *tx.Tx,
		options ...common.Option,
	) error
}

func NewWallet(
	builder Builder,
	signer Signer,
	avaxClient *cchain.Client,
	ethClient *client.Client,
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
	avaxClient *cchain.Client
	ethClient  *client.Client
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
) (*tx.Tx, error) {
	baseFee, err := w.baseFee(options)
	if err != nil {
		return nil, err
	}

	utx, err := w.builder.NewImportTx(chainID, to, baseFee, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*tx.Tx, error) {
	baseFee, err := w.baseFee(options)
	if err != nil {
		return nil, err
	}

	utx, err := w.builder.NewExportTx(chainID, outputs, baseFee, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueUnsignedTx(
	utx tx.Unsigned,
	options ...common.Option,
) (*tx.Tx, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	t, err := SignUnsigned(ctx, w.signer, utx)
	if err != nil {
		return nil, err
	}

	return t, w.IssueTx(t, options...)
}

func (w *wallet) IssueTx(
	t *tx.Tx,
	options ...common.Option,
) error {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	startTime := time.Now()

	if err := w.avaxClient.IssueTx(ctx, t); err != nil {
		return err
	}
	txID := t.ID()

	issuanceDuration := time.Since(startTime)
	if f := ops.IssuanceHandler(); f != nil {
		f(common.IssuanceReceipt{
			ChainAlias: Alias,
			TxID:       txID,
			Duration:   issuanceDuration,
		})
	}

	if ops.AssumeDecided() {
		return w.Backend.AcceptTx(ctx, t)
	}

	if err := w.avaxClient.AwaitTxAccepted(ctx, txID, ops.PollFrequency()); err != nil {
		return err
	}

	if f := ops.ConfirmationHandler(); f != nil {
		totalDuration := time.Since(startTime)
		confirmationDuration := totalDuration - issuanceDuration

		f(common.ConfirmationReceipt{
			ChainAlias:           Alias,
			TxID:                 txID,
			TotalDuration:        totalDuration,
			ConfirmationDuration: confirmationDuration,
		})
	}

	return w.Backend.AcceptTx(ctx, t)
}

func (w *wallet) baseFee(options []common.Option) (gas.Price, error) {
	ops := common.NewOptions(options)
	if baseFee, ok := ops.BaseFee(); ok {
		return baseFee, nil
	}

	return w.ethClient.EstimateBaseFee(ops.Context())
}
