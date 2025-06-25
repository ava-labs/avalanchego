// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type Notifier interface {
	Notify(context.Context, Message) error
}

// NotificationForwarder is a component that listens for notifications from a Subscription,
// and forwards them to a Notifier.
type NotificationForwarder struct {
	Notifier  Notifier
	Subscribe Subscription
	Log       logging.Logger

	running   sync.WaitGroup
	closeChan chan struct{}
	lock      sync.Mutex
	cancel    context.CancelFunc
}

func (nf *NotificationForwarder) Start() {
	nf.running.Add(1)
	nf.closeChan = make(chan struct{})
	go nf.run()
}

func (nf *NotificationForwarder) run() {
	defer nf.running.Done()
	for {
		select {
		case <-nf.closeChan:
			return
		default:
		}

		ctx := nf.setAndGetContext()

		nf.Log.Debug("Subscribing to notifications")
		msg := nf.Subscribe(ctx)
		nf.Log.Debug("Received notification", zap.Stringer("msg", msg))

		select {
		case <-nf.closeChan:
			return
		default:
		}

		if err := nf.Notifier.Notify(ctx, msg); err != nil {
			nf.Log.Error("Failed notifying engine", zap.Error(err))
		}

		nf.cancel()
	}
}

func (nf *NotificationForwarder) setAndGetContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	nf.lock.Lock()
	nf.cancel = cancel
	nf.lock.Unlock()
	return ctx
}

func (nf *NotificationForwarder) Close() {
	defer nf.running.Wait()

	select {
	case <-nf.closeChan:
	default:
		close(nf.closeChan)
		nf.lock.Lock()
		if nf.cancel != nil {
			nf.cancel()
		}
		nf.lock.Unlock()
	}
}
