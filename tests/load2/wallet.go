// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"

	ethereum "github.com/ava-labs/libevm"
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
	pingFrequency time.Duration,
) error {
	startTime := time.Now()
	if err := w.client.SendTransaction(ctx, tx); err != nil {
		return err
	}

	issuanceDuration := time.Since(startTime)
	w.metrics.issue(issuanceDuration)

	if err := awaitTx(ctx, w.client, tx.Hash(), pingFrequency); err != nil {
		return err
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	w.metrics.accept(confirmationDuration, totalDuration)

	w.nonce++

	return nil
}

func awaitTx(
	ctx context.Context,
	client *ethclient.Client,
	txHash common.Hash,
	pingFrequency time.Duration,
) error {
	ticker := time.NewTicker(pingFrequency)
	defer ticker.Stop()

	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				return fmt.Errorf("failed tx: %d", receipt.Status)
			}
			return nil
		}

		if !errors.Is(err, ethereum.NotFound) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
