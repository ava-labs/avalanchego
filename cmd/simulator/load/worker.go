// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type singleAddressTxWorker struct {
	client ethclient.Client

	acceptedNonce uint64
	address       common.Address

	sub      interfaces.Subscription
	newHeads chan *types.Header
}

// NewSingleAddressTxWorker creates and returns a singleAddressTxWorker
func NewSingleAddressTxWorker(ctx context.Context, client ethclient.Client, address common.Address) *singleAddressTxWorker {
	newHeads := make(chan *types.Header)
	tw := &singleAddressTxWorker{
		client:   client,
		address:  address,
		newHeads: newHeads,
	}

	sub, err := client.SubscribeNewHead(ctx, newHeads)
	if err != nil {
		log.Debug("failed to subscribe new heads, falling back to polling", "err", err)
	} else {
		tw.sub = sub
	}

	return tw
}

func (tw *singleAddressTxWorker) IssueTx(ctx context.Context, tx *types.Transaction) error {
	return tw.client.SendTransaction(ctx, tx)
}

func (tw *singleAddressTxWorker) ConfirmTx(ctx context.Context, tx *types.Transaction) error {
	txNonce := tx.Nonce()

	for {
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

		// Update the worker's accepted nonce, so we can check on the next iteration
		// if the transaction has been accepted.
		acceptedNonce, err := tw.client.NonceAt(ctx, tw.address, nil)
		if err != nil {
			return fmt.Errorf("failed to await tx %s nonce %d: %w", tx.Hash(), txNonce, err)
		}
		tw.acceptedNonce = acceptedNonce
	}
}

func (tw *singleAddressTxWorker) Close(ctx context.Context) error {
	if tw.sub != nil {
		tw.sub.Unsubscribe()
	}
	close(tw.newHeads)
	return nil
}
