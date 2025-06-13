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

type Backend interface {
	PrivKey() *ecdsa.PrivateKey
	Nonce() uint64
	ChainID() *big.Int
	Signer() types.Signer
}

type backend struct {
	privKey *ecdsa.PrivateKey
	nonce   uint64
	chainID *big.Int
	signer  types.Signer
}

func newBackend(privKey *ecdsa.PrivateKey, nonce uint64, chainID *big.Int) *backend {
	return &backend{
		privKey: privKey,
		nonce:   nonce,
		chainID: chainID,
		signer:  types.LatestSignerForChainID(chainID),
	}
}

func (b *backend) PrivKey() *ecdsa.PrivateKey {
	return b.privKey
}

func (b *backend) Nonce() uint64 {
	return b.nonce
}

func (b *backend) ChainID() *big.Int {
	return b.chainID
}

func (b *backend) Signer() types.Signer {
	return b.signer
}

type Wallet struct {
	client  *ethclient.Client
	backend *backend
}

func NewWallet(
	client *ethclient.Client,
	privKey *ecdsa.PrivateKey,
	nonce uint64,
	chainID *big.Int,
) *Wallet {
	return &Wallet{
		client:  client,
		backend: newBackend(privKey, nonce, chainID),
	}
}

func (w *Wallet) SendTx(
	ctx context.Context,
	tx *types.Transaction,
	pingFrequency time.Duration,
	issuanceHandler func(time.Duration),
	confirmationHandler func(*types.Receipt, time.Duration),
) error {
	startTime := time.Now()
	if err := w.client.SendTransaction(ctx, tx); err != nil {
		return err
	}

	issuanceDuration := time.Since(startTime)
	issuanceHandler(issuanceDuration)

	receipt, err := awaitTx(ctx, w.client, tx.Hash(), pingFrequency)
	if err != nil {
		return err
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	confirmationHandler(receipt, confirmationDuration)

	w.backend.nonce++

	return nil
}

func (w *Wallet) Backend() Backend {
	return w.backend
}

func awaitTx(
	ctx context.Context,
	client *ethclient.Client,
	txHash common.Hash,
	pingFrequency time.Duration,
) (*types.Receipt, error) {
	ticker := time.NewTicker(pingFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) {
				continue
			}

			return nil, err
		}

		if receipt.Status != 1 {
			return nil, fmt.Errorf("failed tx: %d", receipt.Status)
		}

		return receipt, nil
	}
}
