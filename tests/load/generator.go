// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type Test interface {
	Run(tc tests.TestContext, wallet *Wallet)
}

type Worker struct {
	PrivKey *ecdsa.PrivateKey
	Nonce   uint64
	Client  *ethclient.Client
}

type LoadGenerator struct {
	wallets []*Wallet
	test    Test

	// MaxInFlight is the number of concurrent SendTx calls each wallet may
	// have pending at any time. Defaults to 1 (the historical
	// one-tx-per-wallet behavior). Higher values pipeline issuance for a
	// single sender by spawning N goroutines per wallet that all share the
	// wallet's atomic nonce + shared head subscription.
	MaxInFlight int
}

func NewLoadGenerator(
	workers []Worker,
	chainID *big.Int,
	metricsNamespace string,
	registry *prometheus.Registry,
	test Test,
) (LoadGenerator, error) {
	metrics, err := newMetrics(metricsNamespace, registry)
	if err != nil {
		return LoadGenerator{}, err
	}

	wallets := make([]*Wallet, len(workers))
	for i := range wallets {
		wallets[i] = newWallet(
			workers[i].PrivKey,
			workers[i].Nonce,
			chainID,
			workers[i].Client,
			metrics,
		)
	}

	return LoadGenerator{
		wallets:     wallets,
		test:        test,
		MaxInFlight: 1,
	}, nil
}

func (l LoadGenerator) Run(
	ctx context.Context,
	log logging.Logger,
	loadTimeout time.Duration,
	testTimeout time.Duration,
) {
	eg := &errgroup.Group{}

	if loadTimeout != 0 {
		childCtx, cancel := context.WithTimeout(ctx, loadTimeout)
		ctx = childCtx
		defer cancel()
	}

	// Boot the shared head subscription on each wallet before any test
	// goroutines call SendTx. The head goroutine handles all confirmations
	// for txs issued through the wallet, so it must be live first.
	for _, w := range l.wallets {
		if err := w.Start(ctx); err != nil {
			log.Error("failed to start wallet head subscription", zap.Error(err))
			return
		}
	}

	inFlight := l.MaxInFlight
	if inFlight < 1 {
		inFlight = 1
	}
	for i := range l.wallets {
		for j := 0; j < inFlight; j++ {
			eg.Go(func() error {
				for {
					select {
					case <-ctx.Done():
						return nil
					default:
					}

					execTestWithRecovery(ctx, log, l.test, l.wallets[i], testTimeout)
				}
			})
		}
	}

	_ = eg.Wait()
}

// execTestWithRecovery ensures assertion-related panics encountered during test execution are recovered
// and that deferred cleanups are always executed before returning.
func execTestWithRecovery(ctx context.Context, log logging.Logger, test Test, wallet *Wallet, testTimeout time.Duration) {
	tc := tests.NewTestContext(log)
	defer tc.Recover()
	contextWithTimeout, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	tc.SetDefaultContextParent(contextWithTimeout)
	test.Run(tc, wallet)
}
