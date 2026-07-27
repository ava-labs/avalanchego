// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import "go.uber.org/goleak"

// GoleakOptions ignores known leaking goroutines in libevm. It also includes
// [goleak.IgnoreCurrent].
func GoleakOptions() []goleak.Option {
	return []goleak.Option{
		goleak.IgnoreCurrent(),
		// ChainIndexer.Close() may check if the event loop is active before it
		// is marked as active.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core.(*ChainIndexer).eventLoop"),
		// diskLayer.Release() doesn't properly stop generation.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core/state/snapshot.(*diskLayer).generate"),
		// TxPool.Close() doesn't wait for its loop() method to signal
		// termination.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core/txpool.(*TxPool).loop.func2"),
		// Not all filters subscriptions can be closed after the TxPool is
		// closed.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/eth/filters.(*FilterAPI).Logs.func1.deferwrap1.(*Subscription).Unsubscribe.1"),
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/eth/filters.(*FilterAPI).NewHeads.func1.deferwrap1.(*Subscription).Unsubscribe.1"),
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/eth/filters.(*FilterAPI).NewPendingTransactions.func1.deferwrap1.(*Subscription).Unsubscribe.1"),
	}
}
