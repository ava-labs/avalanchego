// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
)

type Test interface {
	Run(tc tests.TestContext, ctx context.Context, wallet *Wallet)
}

type Worker struct {
	PrivKey *ecdsa.PrivateKey
	Nonce   uint64
	ChainID *big.Int
	Client  *ethclient.Client
}

type LoadGenerator struct {
	wallets []*Wallet
	test    Test
}

func NewLoadGenerator(
	workers []Worker,
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
			workers[i].ChainID,
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
	tc tests.TestContext,
	ctx context.Context,
	loadTimeout time.Duration,
	testTimeout time.Duration,
) {
	wg := sync.WaitGroup{}

	childCtx := ctx
	if loadTimeout != 0 {
		ctx, cancel := context.WithTimeout(ctx, loadTimeout)
		childCtx = ctx
		defer cancel()
	}

	for i := range l.wallets {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-childCtx.Done():
					return
				default:
				}

				ctx, cancel := context.WithTimeout(ctx, testTimeout)
				defer cancel()

				l.test.Run(tc, ctx, l.wallets[i])
			}
		}()
	}

	wg.Wait()
}
