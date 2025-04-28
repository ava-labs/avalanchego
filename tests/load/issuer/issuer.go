// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuer

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

type EthClient interface {
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	EthClientPoll
	EthClientSubscriber
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
	client    EthClient
	websocket bool
	tracker   Tracker
	address   common.Address

	// Internal state
	mutex            sync.Mutex
	issuedTxs        uint64
	lastIssuedNonce  uint64
	inFlightTxHashes []common.Hash
	allIssued        bool
}

func New(client EthClient, websocket bool, tracker Tracker, address common.Address) *Issuer {
	return &Issuer{
		client:    client,
		websocket: websocket,
		tracker:   tracker,
		address:   address,
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
	if i.websocket {
		return i.listenSub(ctx)
	}
	return i.listenPoll(ctx)
}

func (i *Issuer) Stop() {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.allIssued = true
}
