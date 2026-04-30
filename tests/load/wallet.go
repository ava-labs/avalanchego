// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
)

var (
	errTxExecutionFailed = errors.New("transaction accepted but failed to execute entirely")
	errWalletStopped     = errors.New("wallet head subscription stopped before tx confirmed")
)

type Wallet struct {
	privKey *ecdsa.PrivateKey
	// nextNonce is the nonce that ClaimNonce will return next; reserved before
	// signing and incremented atomically so concurrent senders never collide.
	nextNonce atomic.Uint64
	chainID   *big.Int
	signer    types.Signer
	client    *ethclient.Client
	metrics   metrics
	account   common.Address

	// limiter, if non-nil, gates SendTx so the wallet does not exceed the
	// configured target TPS. Shared across all wallets in a LoadGenerator
	// run so the cap is global.
	limiter *tpsLimiter

	// Shared head subscription state. Initialized by Start; mutated by the
	// subscription goroutine and by SendTx callers.
	mu        sync.Mutex
	inFlight  map[uint64]*pendingTx // keyed by tx.Nonce()
	stoppedAt error                 // set when the head goroutine exits
}

// pendingTx is the per-nonce handoff between SendTx and the shared head
// subscription goroutine. SendTx blocks on done; the head goroutine signals it
// after fetching the receipt.
type pendingTx struct {
	hash common.Hash
	done chan error
}

func newWallet(
	privKey *ecdsa.PrivateKey,
	nonce uint64,
	chainID *big.Int,
	client *ethclient.Client,
	metrics metrics,
) *Wallet {
	w := &Wallet{
		privKey:  privKey,
		chainID:  chainID,
		signer:   types.LatestSignerForChainID(chainID),
		client:   client,
		metrics:  metrics,
		account:  crypto.PubkeyToAddress(privKey.PublicKey),
		inFlight: map[uint64]*pendingTx{},
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

// Start opens the shared head subscription and launches the goroutine that
// reads new heads and resolves pending txs. Must be called exactly once before
// any SendTx call. The goroutine exits when ctx is cancelled or the
// subscription errors; any in-flight callers receive the exit error.
func (w *Wallet) Start(ctx context.Context) error {
	headers := make(chan *types.Header, 16)
	sub, err := w.client.SubscribeNewHead(ctx, headers)
	if err != nil {
		return err
	}
	go w.runHeadLoop(ctx, sub, headers)
	return nil
}

func (w *Wallet) runHeadLoop(
	ctx context.Context,
	sub interface {
		Unsubscribe()
		Err() <-chan error
	},
	headers chan *types.Header,
) {
	exit := func(reason error) {
		sub.Unsubscribe()
		w.mu.Lock()
		w.stoppedAt = reason
		pending := w.inFlight
		w.inFlight = map[uint64]*pendingTx{}
		w.mu.Unlock()
		for _, p := range pending {
			p.done <- reason
		}
	}
	for {
		select {
		case <-ctx.Done():
			exit(ctx.Err())
			return
		case err := <-sub.Err():
			if err == nil {
				err = errWalletStopped
			}
			exit(err)
			return
		case h, ok := <-headers:
			if !ok {
				exit(errWalletStopped)
				return
			}
			if err := w.resolveAtHeader(ctx, h); err != nil {
				exit(err)
				return
			}
		}
	}
}

// resolveAtHeader checks the current account nonce at h and resolves every
// pending tx whose nonce has now confirmed.
func (w *Wallet) resolveAtHeader(ctx context.Context, h *types.Header) error {
	latestNonce, err := w.client.NonceAt(ctx, w.account, h.Number)
	if err != nil {
		return err
	}

	w.mu.Lock()
	confirmed := make([]*pendingTx, 0)
	for nonce, p := range w.inFlight {
		if nonce < latestNonce {
			confirmed = append(confirmed, p)
			delete(w.inFlight, nonce)
		}
	}
	w.mu.Unlock()

	for _, p := range confirmed {
		receipt, err := w.client.TransactionReceipt(ctx, p.hash)
		if err != nil {
			p.done <- err
			continue
		}
		if receipt.Status != types.ReceiptStatusSuccessful {
			p.done <- errTxExecutionFailed
			continue
		}
		p.done <- nil
	}
	return nil
}

func (w *Wallet) SendTx(
	ctx context.Context,
	tx *types.Transaction,
) error {
	pending := &pendingTx{
		hash: tx.Hash(),
		done: make(chan error, 1),
	}

	w.mu.Lock()
	if w.stoppedAt != nil {
		err := w.stoppedAt
		w.mu.Unlock()
		return err
	}
	w.inFlight[tx.Nonce()] = pending
	w.mu.Unlock()

	defer func() {
		// Best-effort cleanup if we exit before the head goroutine signals us
		// (e.g., SendTransaction failed or ctx cancelled). done is buffered so
		// a late signal won't block.
		w.mu.Lock()
		if cur, ok := w.inFlight[tx.Nonce()]; ok && cur == pending {
			delete(w.inFlight, tx.Nonce())
		}
		w.mu.Unlock()
	}()

	if err := w.limiter.Wait(ctx); err != nil {
		return err
	}

	startTime := time.Now()
	if err := w.client.SendTransaction(ctx, tx); err != nil {
		return err
	}
	issuanceDuration := time.Since(startTime)
	w.metrics.issue(issuanceDuration)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-pending.done:
		if err != nil {
			return err
		}
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	w.metrics.accept(confirmationDuration, totalDuration)
	return nil
}
