// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
)

type ethereumTxWorker struct {
	client *ethclient.Client

	acceptedNonce uint64
	address       common.Address

	newHeads chan *types.Header
}

// NewSingleAddressTxWorker creates and returns a new ethereumTxWorker that confirms transactions by checking the latest
// nonce of [address] and assuming any transaction with a lower nonce was already accepted.
func NewSingleAddressTxWorker(client *ethclient.Client, address common.Address) *ethereumTxWorker {
	newHeads := make(chan *types.Header)
	tw := &ethereumTxWorker{
		client:   client,
		address:  address,
		newHeads: newHeads,
	}

	return tw
}

// NewTxReceiptWorker creates and returns a new ethereumTxWorker that confirms transactions by checking for the
// corresponding transaction receipt.
func NewTxReceiptWorker(client *ethclient.Client) *ethereumTxWorker {
	newHeads := make(chan *types.Header)
	tw := &ethereumTxWorker{
		client:   client,
		newHeads: newHeads,
	}

	return tw
}

func (tw *ethereumTxWorker) IssueTx(ctx context.Context, tx *types.Transaction) error {
	return tw.client.SendTransaction(ctx, tx)
}

func (tw *ethereumTxWorker) ConfirmTx(ctx context.Context, tx *types.Transaction) error {
	if tw.address == (common.Address{}) {
		return tw.confirmTxByReceipt(ctx, tx)
	}
	return tw.confirmTxByNonce(ctx, tx)
}

func (tw *ethereumTxWorker) confirmTxByNonce(ctx context.Context, tx *types.Transaction) error {
	txNonce := tx.Nonce()

	for {
		acceptedNonce, err := tw.client.NonceAt(ctx, tw.address, nil)
		if err != nil {
			return fmt.Errorf("failed to await tx %s nonce %d: %w", tx.Hash(), txNonce, err)
		}
		tw.acceptedNonce = acceptedNonce

		log.Debug("confirming tx", "txHash", tx.Hash(), "txNonce", txNonce, "acceptedNonce", tw.acceptedNonce)
		// If the is less than what has already been accepted, the transaction is confirmed
		if txNonce < tw.acceptedNonce {
			return nil
		}

		select {
		case <-tw.newHeads:
		case <-time.After(time.Second):
		case <-ctx.Done():
			return fmt.Errorf("failed to await tx %s nonce %d: %w", tx.Hash(), txNonce, ctx.Err())
		}
	}
}

func (tw *ethereumTxWorker) confirmTxByReceipt(ctx context.Context, tx *types.Transaction) error {
	for {
		_, err := tw.client.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			return nil
		}
		log.Debug("no tx receipt", "txHash", tx.Hash(), "nonce", tx.Nonce(), "err", err)

		select {
		case <-tw.newHeads:
		case <-time.After(time.Second):
		case <-ctx.Done():
			return fmt.Errorf("failed to await tx %s nonce %d: %w", tx.Hash(), tx.Nonce(), ctx.Err())
		}
	}
}

func (tw *ethereumTxWorker) LatestHeight(ctx context.Context) (uint64, error) {
	return tw.client.BlockNumber(ctx)
}
