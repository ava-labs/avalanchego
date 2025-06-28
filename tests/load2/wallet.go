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
	"github.com/ava-labs/libevm/ethclient"

	ethereum "github.com/ava-labs/libevm"
)

type Wallet struct {
	metrics Metrics
	client  *ethclient.Client
	privKey *ecdsa.PrivateKey
	nonce   uint64
	chainID *big.Int
	signer  types.Signer
}

func NewWallet(
	metrics Metrics,
	client *ethclient.Client,
	privKey *ecdsa.PrivateKey,
	nonce uint64,
	chainID *big.Int,
) *Wallet {
	return &Wallet{
		metrics: metrics,
		client:  client,
		privKey: privKey,
		nonce:   nonce,
		chainID: chainID,
		signer:  types.LatestSignerForChainID(chainID),
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
	w.metrics.Issue(issuanceDuration)

	if err := awaitTx(ctx, w.client, tx.Hash(), pingFrequency); err != nil {
		return err
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	w.metrics.Accept(confirmationDuration, totalDuration)

	w.nonce++

	return nil
}

func (w *Wallet) PrivKey() *ecdsa.PrivateKey {
	return w.privKey
}

func (w *Wallet) Nonce() uint64 {
	return w.nonce
}

func (w *Wallet) ChainID() *big.Int {
	return w.chainID
}

func (w *Wallet) Signer() types.Signer {
	return w.signer
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
