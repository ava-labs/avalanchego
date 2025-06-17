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

	"github.com/ava-labs/avalanchego/utils/logging"
)

const pingFrequency = time.Millisecond

type Tracker struct {
	lock sync.RWMutex

	totalGasUsed uint64
	txsIssued    uint64
	txsAccepted  uint64
}

func (t *Tracker) Issue(time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsIssued++
}

func (t *Tracker) Accept(receipt *types.Receipt, _ time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsAccepted++
	t.totalGasUsed += receipt.GasUsed
}

func (t *Tracker) TotalGasUsed() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.totalGasUsed
}

type TxBuilder func(Backend) (*types.Transaction, error)

type Generator struct {
	log        logging.Logger
	wallets    []Wallet
	txBuilders []TxBuilder
}

func NewGenerator(
	log logging.Logger,
	wallets []Wallet,
	txBuilders []TxBuilder,
) (Generator, error) {
	if len(wallets) != len(txBuilders) {
		return Generator{}, fmt.Errorf(
			"wallet and tx builder count mismatch: got %d wallets and %d txBuilders",
			len(wallets),
			len(txBuilders),
		)
	}

	return Generator{
		log:        log,
		wallets:    wallets,
		txBuilders: txBuilders,
	}, nil
}

func (g Generator) Run(ctx context.Context) error {
	tracker := &Tracker{}
	issuerGroup, childCtx := errgroup.WithContext(ctx)

	for i := range len(g.wallets) {
		issuerGroup.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				// Build tx
				tx, err := g.txBuilders[i](g.wallets[i].Backend())
				if err != nil {
					return err
				}

				// Issue and await tx
				if err := g.wallets[i].SendTx(
					childCtx,
					tx,
					pingFrequency,
					func(d time.Duration) {
						tracker.Issue(d)
					},
					func(r *types.Receipt, d time.Duration) {
						tracker.Accept(r, d)
					},
				); err != nil {
					return err
				}
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
