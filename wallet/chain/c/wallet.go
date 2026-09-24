// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
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
	) (*atomic.Tx, error)

	// IssueExportTx creates, signs, and issues an export transaction that
	// attempts to send all the provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	IssueExportTx(
		chainID ids.ID,
		outputs []*secp256k1fx.TransferOutput,
		options ...common.Option,
	) (*atomic.Tx, error)

	// IssueUnsignedAtomicTx signs and issues the unsigned tx.
	IssueUnsignedAtomicTx(
		utx atomic.UnsignedAtomicTx,
		options ...common.Option,
	) (*atomic.Tx, error)

	// IssueAtomicTx issues the signed tx.
	IssueAtomicTx(
		tx *atomic.Tx,
		options ...common.Option,
	) error
}

func NewWallet(
	builder Builder,
	signer Signer,
	avaxClient *cchain.Client,
	ethClient *ethclient.Client,
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
	ethClient  *ethclient.Client
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
) (*atomic.Tx, error) {
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
) (*atomic.Tx, error) {
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
	utx atomic.UnsignedAtomicTx,
	options ...common.Option,
) (*atomic.Tx, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	tx, err := SignUnsignedAtomic(ctx, w.signer, utx)
	if err != nil {
		return nil, err
	}

	return tx, w.IssueAtomicTx(tx, options...)
}

func (w *wallet) IssueAtomicTx(
	atx *atomic.Tx,
	options ...common.Option,
) error {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	startTime := time.Now()

	// The coreth and SAE atomic tx codecs are wire compatible.
	t, err := tx.Parse(atx.SignedBytes())
	if err != nil {
		return fmt.Errorf("parsing atomic tx: %w", err)
	}
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
		return w.Backend.AcceptAtomicTx(ctx, atx)
	}

	if err := awaitTxAccepted(ctx, w.avaxClient, txID, ops.PollFrequency()); err != nil {
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

	return w.Backend.AcceptAtomicTx(ctx, atx)
}

// txGetter returns an accepted atomic tx and its block height.
type txGetter interface {
	GetTx(ctx context.Context, txID ids.ID, options ...rpc.Option) (*tx.Tx, uint64, error)
}

// awaitTxAccepted polls avax.getAtomicTx every freq until txID is accepted or
// ctx is cancelled.
func awaitTxAccepted(ctx context.Context, c txGetter, txID ids.ID, freq time.Duration) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		_, _, err := c.GetTx(ctx, txID)
		if err == nil {
			return nil
		}
		if !isTxNotFound(err) {
			return err
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// isTxNotFound reports whether err means the node has not accepted the tx yet.
// SAE returns "fetching tx: reading tx: not found" and coreth returns
// "could not find tx <txID>".
func isTxNotFound(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "reading tx: not found") || strings.Contains(msg, "could not find tx")
}

func (w *wallet) baseFee(options []common.Option) (*big.Int, error) {
	ops := common.NewOptions(options)
	baseFee := ops.BaseFee(nil)
	if baseFee != nil {
		return baseFee, nil
	}

	ctx := ops.Context()
	var fee hexutil.Big
	if err := w.ethClient.Client().CallContext(ctx, &fee, "eth_baseFee"); err != nil {
		return nil, err
	}
	return (*big.Int)(&fee), nil
}
