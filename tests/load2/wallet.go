// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
)

type Wallet struct {
	privKey *ecdsa.PrivateKey
	nonce   uint64
	chainID *big.Int
	signer  types.Signer
	client  *ethclient.Client
	metrics metrics
}

func newWallet(
	privKey *ecdsa.PrivateKey,
	nonce uint64,
	chainID *big.Int,
	client *ethclient.Client,
	metrics metrics,
) *Wallet {
	return &Wallet{
		privKey: privKey,
		nonce:   nonce,
		chainID: chainID,
		signer:  types.LatestSignerForChainID(chainID),
		client:  client,
		metrics: metrics,
	}
}

func (w *Wallet) SendTx(
	ctx context.Context,
	tx *types.Transaction,
) error {
	// start listening for blocks
	headers := make(chan *types.Header)
	sub, err := w.client.SubscribeNewHead(ctx, headers)
	if err != nil {
		return err
	}

	defer func() {
		sub.Unsubscribe()

		// wait for err chan to close before safely closing headers
		<-sub.Err()
		close(headers)
	}()

	startTime := time.Now()
	if err := w.client.SendTransaction(ctx, tx); err != nil {
		return err
	}

	issuanceDuration := time.Since(startTime)
	w.metrics.issue(issuanceDuration)

	err = w.awaitTx(
		ctx,
		headers,
		sub.Err(),
		tx.Hash(),
	)
	if err != nil {
		return err
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	w.metrics.accept(confirmationDuration, totalDuration)

	w.nonce++

	return nil
}

func (w Wallet) awaitTx(
	ctx context.Context,
	headers chan *types.Header,
	errs <-chan error,
	txHash common.Hash,
) error {
	account := crypto.PubkeyToAddress(w.privKey.PublicKey)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return err
		case h := <-headers:
			latestNonce, err := w.client.NonceAt(
				ctx,
				account,
				h.Number,
			)
			if err != nil {
				return err
			}

			if latestNonce == w.nonce+1 {
				receipt, err := w.client.TransactionReceipt(ctx, txHash)
				if err != nil {
					return err
				}

				if receipt.Status != types.ReceiptStatusSuccessful {
					return fmt.Errorf("failed tx: %d", receipt.Status)
				}

				return nil
			}
		}
	}
}
