// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type tracker struct {
	lock sync.RWMutex

	totalGasUsed uint64
	txsIssued    uint64
	txsAccepted  uint64
}

func (t *tracker) Issue(time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsIssued++
}

func (t *tracker) Accept(receipt *types.Receipt, _ time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsAccepted++
	t.totalGasUsed += receipt.GasUsed
}

func (t *tracker) TotalGasUsed() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.totalGasUsed
}

type TxTest func(tests.TestContext, context.Context, *Wallet)

type Generator struct {
	log     logging.Logger
	wallets []*Wallet
	txTests []TxTest
}

func NewGenerator(
	log logging.Logger,
	wallets []*Wallet,
	txTests []TxTest,
) (Generator, error) {
	if len(wallets) != len(txTests) {
		return Generator{}, fmt.Errorf(
			"wallet and tx builder count mismatch: got %d wallets and %d txBuilders",
			len(wallets),
			len(txTests),
		)
	}

	return Generator{
		log:     log,
		wallets: wallets,
		txTests: txTests,
	}, nil
}

func (g Generator) Run(tc tests.TestContext, ctx context.Context) error {
	tracker := &tracker{}
	issuerGroup, childCtx := errgroup.WithContext(ctx)

	for i := range len(g.wallets) {
		issuerGroup.Go(func() error {
			for {
				select {
				case <-childCtx.Done():
					return childCtx.Err()
				default:
				}

				g.txTests[i](tc, ctx, g.wallets[i])
			}
		})
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	prevTotalGasUsed := uint64(0)
	prevTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		currTotalGasUsed := tracker.TotalGasUsed()
		currTime := time.Now()

		gps := computeDiff(prevTotalGasUsed, currTotalGasUsed, currTime.Sub(prevTime))
		g.log.Info("stats", zap.Uint64("gps", gps))

		prevTotalGasUsed = currTotalGasUsed
		prevTime = currTime
	}
}

func computeDiff(initial uint64, final uint64, duration time.Duration) uint64 {
	if duration <= 0 {
		return 0
	}

	numTxs := final - initial
	tps := float64(numTxs) / duration.Seconds()

	return uint64(tps)
}
