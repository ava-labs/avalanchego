// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ethereum "github.com/ava-labs/libevm"
	ethcommon "github.com/ava-labs/libevm/common"
)

var (
	_ Wallet = (*wallet)(nil)
	_ Wallet = (*walletWithOptions)(nil)
)

type Wallet interface {
	SendTx(context.Context, *types.Transaction, ...common.Option) error
	PrivKey() *ecdsa.PrivateKey
	ChainID() *big.Int
	Nonce() uint64
	Signer() types.Signer
}

type wallet struct {
	client  *ethclient.Client
	privKey *ecdsa.PrivateKey
	nonce   uint64
	chainID *big.Int
	signer  types.Signer
}

func NewWallet(
	client *ethclient.Client,
	privKey *ecdsa.PrivateKey,
	nonce uint64,
	chainID *big.Int,
) Wallet {
	return &wallet{
		client:  client,
		privKey: privKey,
		nonce:   nonce,
		chainID: chainID,
		signer:  types.LatestSignerForChainID(chainID),
	}
}

func (w *wallet) SendTx(
	ctx context.Context,
	tx *types.Transaction,
	ops ...common.Option,
) error {
	options := common.NewOptions(ops)

	startTime := time.Now()
	if err := w.client.SendTransaction(ctx, tx); err != nil {
		return err
	}

	issuanceDuration := time.Since(startTime)
	if f := options.IssuanceHandler(); f != nil {
		f(common.IssuanceReceipt{
			Duration: issuanceDuration,
		})
	}

	if err := awaitTx(ctx, w.client, tx.Hash(), options.PollFrequency()); err != nil {
		return err
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	if f := options.ConfirmationHandler(); f != nil {
		f(common.ConfirmationReceipt{
			ConfirmationDuration: confirmationDuration,
			TotalDuration:        totalDuration,
			TxID:                 ids.ID(tx.Hash()),
		})
	}

	w.nonce++

	return nil
}

func (w *wallet) PrivKey() *ecdsa.PrivateKey {
	return w.privKey
}

func (w *wallet) Nonce() uint64 {
	return w.nonce
}

func (w *wallet) ChainID() *big.Int {
	return w.chainID
}

func (w *wallet) Signer() types.Signer {
	return w.signer
}

type walletWithOptions struct {
	Wallet
	options []common.Option
}

func NewWalletWithOptions(
	wallet Wallet,
	options ...common.Option,
) Wallet {
	return &walletWithOptions{
		Wallet:  wallet,
		options: options,
	}
}

func (w *walletWithOptions) SendTransaction(
	ctx context.Context,
	tx *types.Transaction,
	ops ...common.Option,
) error {
	return w.Wallet.SendTx(
		ctx,
		tx,
		common.UnionOptions(w.options, ops)...,
	)
}

func awaitTx(
	ctx context.Context,
	client *ethclient.Client,
	txHash ethcommon.Hash,
	pingFrequency time.Duration,
) error {
	ticker := time.NewTicker(pingFrequency)
	defer ticker.Stop()

	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			if receipt.Status != 1 {
				return fmt.Errorf("failed tx: %d", receipt.Status)
			}
			return nil
		}

		if err != ethereum.NotFound {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
