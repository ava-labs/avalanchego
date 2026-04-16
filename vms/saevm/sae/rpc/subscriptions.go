// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"sync"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
)

func (b *backend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.Set.Pool.SubscribeTransactions(ch, true)
}

// A number of subscriptions don't make sense in SAE so are no-ops. The lack of
// reorgs makes chain-side and log-removal events impossible. As "pending"
// refers to accepted but not executed blocks, pending logs are an oxymoron.

func (*backend) SubscribeChainSideEvent(chan<- core.ChainSideEvent) event.Subscription {
	return newNoopSubscription()
}

func (*backend) SubscribeRemovedLogsEvent(chan<- core.RemovedLogsEvent) event.Subscription {
	return newNoopSubscription()
}

func (*backend) SubscribePendingLogsEvent(chan<- []*types.Log) event.Subscription {
	return newNoopSubscription()
}

type noopSubscription struct {
	once sync.Once
	err  chan error
}

func newNoopSubscription() *noopSubscription {
	return &noopSubscription{
		err: make(chan error),
	}
}

func (s *noopSubscription) Err() <-chan error {
	return s.err
}

func (s *noopSubscription) Unsubscribe() {
	s.once.Do(func() {
		close(s.err)
	})
}
