// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type Test interface {
	Run(tc tests.TestContext, ctx context.Context, wallet *Wallet)
}

type Worker struct {
	PrivKey *ecdsa.PrivateKey
	Nonce   uint64
	Client  *ethclient.Client
}

type LoadGenerator struct {
	wallets []*Wallet
	test    Test
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
		wallets: wallets,
		test:    test,
	}, nil
}

func (l LoadGenerator) Run(
	ctx context.Context,
	log logging.Logger,
	loadTimeout time.Duration,
	testTimeout time.Duration,
) {
	eg := &errgroup.Group{}

	childCtx := ctx
	if loadTimeout != 0 {
		ctx, cancel := context.WithTimeout(ctx, loadTimeout)
		childCtx = ctx
		defer cancel()
	}

	for i := range l.wallets {
		eg.Go(func() error {
			for {
				select {
				case <-childCtx.Done():
					return nil
				default:
				}

				ctx, cancel := context.WithTimeout(ctx, testTimeout)
				defer cancel()

				execTestWithRecovery(ctx, log, l.test, l.wallets[i])
			}
		})
	}

	_ = eg.Wait()
}

// execTestWithRecovery ensures assertion-related panics encountered during test execution are recovered
// and that deferred cleanups are always executed before returning.
func execTestWithRecovery(ctx context.Context, log logging.Logger, test Test, wallet *Wallet) {
	tc := tests.NewTestContext(log)
	defer tc.Recover()
	test.Run(tc, ctx, wallet)
}
