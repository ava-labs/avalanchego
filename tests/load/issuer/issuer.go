// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuer

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"go.uber.org/zap"
)

type Tracker interface {
	IssueStart(txHash common.Hash)
	IssueEnd(txHash common.Hash)
	ObserveConfirmed(txHash common.Hash)
	// ObserveBlock observes a new block with the given number.
	// It should be called when a new block is received.
	// It should ignore the block if the number has already been observed.
	// It may also ignore the first block observed when it comes to time,
	// given the missing information on the time start for the first block.
	ObserveBlock(number uint64)
}

// Issuer issues transactions to a node and waits for them to be accepted (or failed).
// It
type Issuer struct {
	// Injected parameters
	client    ethclient.Client
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

func New(client ethclient.Client, websocket bool, tracker Tracker, address common.Address) *Issuer {
	return &Issuer{
		client:    client,
		websocket: websocket,
		tracker:   tracker,
		address:   address,
	}
}

func (i *Issuer) IssueTx(tc tests.TestContext, ctx context.Context, tx *types.Transaction) error {
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
	tc.Log().Info("issued transaction",
		zap.String("sender", i.address.Hex()),
		zap.Uint64("nonce", tx.Nonce()),
		zap.String("txHash", txHash.Hex()))

	i.tracker.IssueEnd(txHash)
	i.inFlightTxHashes = append(i.inFlightTxHashes, txHash)
	i.issuedTxs++
	i.lastIssuedNonce = txNonce
	return nil
}

func (i *Issuer) Listen(tc tests.TestContext, ctx context.Context) error {
	return i.listenPoll(tc, ctx)
}

func (i *Issuer) Stop() {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.allIssued = true
}
