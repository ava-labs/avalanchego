// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package listen

import (
	"context"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

type EthClient interface {
	EthClientPoll
	EthClientSubscriber
}

type Tracker interface {
	ObserveConfirmed(txHash common.Hash)
	// ObserveBlock observes a new block with the given number.
	// It should be called when a new block is received.
	// It should ignore the block if the number has already been observed.
	// It may also ignore the first block observed when it comes to time,
	// given the missing information on the time start for the first block.
	ObserveBlock(number uint64)
}

// Listener listens for transaction confirmations from a node.
type Listener struct {
	// Injected parameters
	client    EthClient
	websocket bool
	tracker   Tracker
	txTarget  uint64
	address   common.Address

	// Internal state
	mutex            sync.Mutex
	issued           uint64
	lastIssuedNonce  uint64
	inFlightTxHashes []common.Hash
}

func New(client EthClient, websocket bool, tracker Tracker, txTarget uint64, address common.Address) *Listener {
	return &Listener{
		client:    client,
		websocket: websocket,
		tracker:   tracker,
		txTarget:  txTarget,
		address:   address,
	}
}

func (l *Listener) Listen(ctx context.Context) error {
	if l.websocket {
		return l.listenSub(ctx)
	}
	return l.listenPoll(ctx)
}

func (l *Listener) RegisterIssued(tx *types.Transaction) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.issued++
	l.lastIssuedNonce = tx.Nonce()
	l.inFlightTxHashes = append(l.inFlightTxHashes, tx.Hash())
}
