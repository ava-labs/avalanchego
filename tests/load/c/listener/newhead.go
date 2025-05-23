// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package listener

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/core/types"

	ethereum "github.com/ava-labs/libevm"
)

type NewHeadSubscriber interface {
	SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)
}

type headNotifier struct {
	client       NewHeadSubscriber
	listenStop   chan<- struct{}
	listenDone   <-chan struct{}
	subscription ethereum.Subscription

	stopMutex sync.Mutex
	stopped   bool
}

func newHeadNotifier(client NewHeadSubscriber) *headNotifier {
	return &headNotifier{
		client: client,
	}
}

func (n *headNotifier) start() (newHead <-chan struct{}, runError <-chan error, err error) {
	newHeadCh := make(chan struct{})

	listenStop := make(chan struct{})
	n.listenStop = listenStop
	listenDone := make(chan struct{})
	n.listenDone = listenDone
	ready := make(chan struct{})

	subscriptionCh := make(chan *types.Header)
	// Note the subscription gets stopped with subscription.Unsubscribe() and does
	// not rely on its subscribe context cancelation.
	subscription, err := n.client.SubscribeNewHead(context.Background(), subscriptionCh)
	if err != nil {
		return nil, nil, fmt.Errorf("subscribing to new head: %w", err)
	}
	go subscriptionChToSignal(listenStop, listenDone, ready, subscriptionCh, newHeadCh)
	<-ready
	n.subscription = subscription
	return newHeadCh, n.makeRunErrCh(), nil
}

func (n *headNotifier) stop() {
	n.stopMutex.Lock()
	defer n.stopMutex.Unlock()
	if !n.stopped {
		n.subscription.Unsubscribe()
		close(n.listenStop)
		n.stopped = true
	}
	<-n.listenDone
}

func subscriptionChToSignal(listenStop <-chan struct{}, listenDone, ready chan<- struct{},
	subCh <-chan *types.Header, newHeadCh chan<- struct{},
) {
	defer close(listenDone)
	close(ready)
	for {
		select {
		case <-listenStop:
			return
		case <-subCh:
			select {
			case newHeadCh <- struct{}{}:
			case <-listenStop:
				return
			}
		}
	}
}

// makeRunErrCh makes sure the [headNotifier] fully stops when
// a subscription error is encountered.
func (n *headNotifier) makeRunErrCh() <-chan error {
	errCh := make(chan error)
	go func() {
		err, ok := <-n.subscription.Err()
		if !ok {
			// channel is closed when [ethereum.Subscription] `Unsubscribe`
			// is called within [Issuer.stopForwarding].
			return
		}
		n.stop()
		errCh <- fmt.Errorf("subscription error: %w", err)
	}()
	return errCh
}
