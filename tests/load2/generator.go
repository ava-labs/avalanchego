// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/tests"
)

type Test interface {
	Run(tc tests.TestContext, ctx context.Context, wallet *Wallet)
}

type LoadGenerator struct {
	wallets []*Wallet
	test    Test
}

func NewLoadGenerator(wallets []*Wallet, test Test) (LoadGenerator, error) {
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
