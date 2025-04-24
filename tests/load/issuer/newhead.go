// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuer

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

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
}

func newHeadNotifier(client NewHeadSubscriber) *headNotifier {
	return &headNotifier{
		client: client,
	}
}

func (n *headNotifier) start(ctx context.Context) (newHead <-chan struct{}, runError <-chan error) {
	newHeadSignal := make(chan struct{})

	listenStop := make(chan struct{})
	n.listenStop = listenStop
	listenDone := make(chan struct{})
	n.listenDone = listenDone
	ready := make(chan struct{})

	subscriptionCh := make(chan *types.Header)
	subscription, err := n.client.SubscribeNewHead(ctx, subscriptionCh)
	if err != nil {
		log.Debug("failed to subscribe new heads, falling back to polling", "err", err)
		go periodicNotify(listenStop, listenDone, ready, newHeadSignal)
		<-ready
		return newHeadSignal, nil // no possible error at runtime
	}
	go subscriptionChToSignal(listenStop, listenDone, ready, subscriptionCh, newHeadSignal)
	<-ready
	n.subscription = subscription
	return newHeadSignal, n.makeRunErrCh()
}

func (n *headNotifier) stop() {
	if n.subscription != nil {
		n.subscription.Unsubscribe()
	}
	close(n.listenStop)
	<-n.listenDone
}

// periodicNotify is the fallback if the connection is not a websocket.
func periodicNotify(listenStop <-chan struct{}, listenDone, ready chan<- struct{},
	newHeadSignal chan<- struct{},
) {
	defer close(listenDone)
	ticker := time.NewTicker(time.Second)
	close(ready)
	for {
		select {
		case <-listenStop:
			ticker.Stop()
			return
		case <-ticker.C:
			newHeadSignal <- struct{}{}
		}
	}
}

func subscriptionChToSignal(listenStop <-chan struct{}, listenDone, ready chan<- struct{},
	subCh <-chan *types.Header, newHeadSignal chan<- struct{},
) {
	defer close(listenDone)
	close(ready)
	for {
		select {
		case <-listenStop:
			return
		case <-subCh:
			newHeadSignal <- struct{}{}
		}
	}
}

// makeRunErrCh makes sure the [newHeadNotifyer] fully stops when
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
