// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"fmt"
	"math/big"
	"time"

	"github.com/gdexlab/go-render/render"
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/plugin/evm"

	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Wallet = (*wallet)(nil)

// errNotCommitted = errors.New("not committed")

type Wallet interface {
	Context

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

	// IssueUnsignedTx signs and issues the unsigned tx.
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
	tx, err := w.signer.SignUnsignedAtomic(ctx, utx)
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

	pollFrequency := ops.PollFrequency()
	ticker := time.NewTicker(pollFrequency)
	defer ticker.Stop()

	for {
		status, err := w.avaxClient.GetAtomicTxStatus(ctx, txID)
		_, _ = fmt.Fprintf(ginkgo.GinkgoWriter, "[DEBUG]:STATUS %+v, %+v", status, err)

		if err != nil {
			return err
		}
		opOutput := render.AsCode(ops)
		switch status {
		case evm.Accepted:
			_, _ = fmt.Fprintf(ginkgo.GinkgoWriter, "[DEBUG]:ACCEPTED ATOMIC TX %+v, %+v", tx, ops)
			return w.Backend.AcceptAtomicTx(ctx, tx)
		case evm.Dropped, evm.Unknown:
			return fmt.Errorf("not committed %s, %s, %+v", status, txID, opOutput)
		}

		// The tx is Processing.

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
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
