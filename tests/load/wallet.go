// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
)

var errTxExecutionFailed = errors.New("transaction accepted but failed to execute entirely")

type Wallet struct {
	privKey *ecdsa.PrivateKey
	// nextNonce is the nonce that ClaimNonce will return next; reserved before
	// signing and incremented atomically so concurrent senders never collide.
	nextNonce atomic.Uint64
	chainID   *big.Int
	signer    types.Signer
	client    *ethclient.Client
	metrics   metrics
}

func newWallet(
	privKey *ecdsa.PrivateKey,
	nonce uint64,
	chainID *big.Int,
	client *ethclient.Client,
	metrics metrics,
) *Wallet {
	w := &Wallet{
		privKey: privKey,
		chainID: chainID,
		signer:  types.LatestSignerForChainID(chainID),
		client:  client,
		metrics: metrics,
	}
	w.nextNonce.Store(nonce)
	return w
}

// ClaimNonce reserves the next sequential nonce for this Wallet. Safe for
// concurrent callers. Each successful claim must be followed by a tx using
// that nonce; if the tx never reaches the chain, subsequent txs will stall
// waiting for the gap to fill.
func (w *Wallet) ClaimNonce() uint64 {
	return w.nextNonce.Add(1) - 1
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
		tx.Nonce(),
		tx.Hash(),
	)
	if err != nil {
		return err
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	w.metrics.accept(confirmationDuration, totalDuration)

	return nil
}

func (w *Wallet) awaitTx(
	ctx context.Context,
	headers chan *types.Header,
	errs <-chan error,
	txNonce uint64,
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

			// Once the on-chain account nonce has advanced past our tx's
			// nonce, our tx has been included in some block. Fetch the
			// receipt to distinguish success from execution-failure.
			if latestNonce > txNonce {
				receipt, err := w.client.TransactionReceipt(ctx, txHash)
				if err != nil {
					return err
				}

				if receipt.Status != types.ReceiptStatusSuccessful {
					return errTxExecutionFailed
				}

				return nil
			}
		}
	}
}
