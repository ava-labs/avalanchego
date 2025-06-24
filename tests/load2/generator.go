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
	Run(tests.TestContext, context.Context, *Wallet)
}

type Generator struct {
	wallets []*Wallet
	test    Test
}

func NewGenerator(
	wallets []*Wallet,
	test Test,
) (Generator, error) {
	return Generator{
		wallets: wallets,
		test:    test,
	}, nil
}

func (g Generator) Run(
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

	for i := range g.wallets {
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

				g.test.Run(tc, ctx, g.wallets[i])
			}
		}()
	}

	wg.Wait()
}
