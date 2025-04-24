// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuer

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

type EthClient interface {
	NewHeadSubscriber
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	NonceAt(ctx context.Context, addr common.Address, blockNumber *big.Int) (uint64, error)
}

type Tracker interface {
	IssueStart(txHash common.Hash)
	IssueEnd(txHash common.Hash)
	ObserveConfirmed(txHash common.Hash)
}

// Issuer issues transactions to a node and waits for them to be accepted (or failed).
// It
type Issuer struct {
	// Injected parameters
	client  EthClient
	tracker Tracker
	address common.Address

	// Internal state
	mutex            sync.Mutex
	issuedTxs        uint64
	lastIssuedNonce  uint64
	inFlightTxHashes []common.Hash
	allIssued        bool
}

func New(client EthClient, tracker Tracker, address common.Address) *Issuer {
	return &Issuer{
		client:  client,
		tracker: tracker,
		address: address,
	}
}

func (i *Issuer) IssueTx(ctx context.Context, tx *types.Transaction) error {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	txHash, txNonce := tx.Hash(), tx.Nonce()
	if txNonce > 0 && txNonce != i.lastIssuedNonce+1 {
		return fmt.Errorf("transaction nonce %d is not equal to the last issued nonce %d plus one", txNonce, i.lastIssuedNonce)
	}
	i.tracker.IssueStart(txHash)
	if err := i.client.SendTransaction(ctx, tx); err != nil {
		return err
	}
	i.tracker.IssueEnd(txHash)
	i.inFlightTxHashes = append(i.inFlightTxHashes, txHash)
	i.issuedTxs++
	i.lastIssuedNonce = txNonce
	return nil
}

func (i *Issuer) Listen(ctx context.Context) error {
	headNotifier := newHeadNotifier(i.client)
	newHeadSignal, notifierErrCh := headNotifier.start(ctx)
	defer headNotifier.stop()

	for {
		blockNumber := (*big.Int)(nil)
		nonce, err := i.client.NonceAt(ctx, i.address, blockNumber)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("checking last block account nonce for address %s: %w", i.address, err)
		}

		i.mutex.Lock()
		confirmed := uint64(len(i.inFlightTxHashes))
		if nonce < i.lastIssuedNonce { // lagging behind last issued nonce
			lag := i.lastIssuedNonce - nonce
			confirmed -= lag
		}
		for index := range confirmed {
			txHash := i.inFlightTxHashes[index]
			i.tracker.ObserveConfirmed(txHash)
		}
		i.inFlightTxHashes = i.inFlightTxHashes[confirmed:]
		finished := i.allIssued && len(i.inFlightTxHashes) == 0
		if finished {
			i.mutex.Unlock()
			return nil
		}
		i.mutex.Unlock()

		select {
		case <-ctx.Done():
			return nil
		case err := <-notifierErrCh:
			return fmt.Errorf("new head notifier failed: %w", err)
		case <-newHeadSignal:
		}
	}
}

func (i *Issuer) Stop() {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.allIssued = true
}
