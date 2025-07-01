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
	Engine    Notifier
	Subscribe Subscription
	Log       logging.Logger

	running   sync.WaitGroup
	closeChan chan struct{}
	lock      sync.Mutex
	closeFunc context.CancelFunc
}

func (nf *NotificationForwarder) Start() {
	nf.running.Add(1)
	nf.closeChan = make(chan struct{})
	go nf.run()
}

func (nf *NotificationForwarder) run() {
	defer nf.running.Done()
	for {
		nf.forwardNotification()
		select {
		case <-nf.closeChan:
			return
		default:
		}
	}
}

func (nf *NotificationForwarder) forwardNotification() {
	ctx := nf.setAndGetContext()
	defer nf.cancelContext()

	select {
	case <-nf.closeChan:
		return
	default:
	}

	nf.Log.Debug("Subscribing to notifications")
	msg, err := nf.Subscribe(ctx)
	if err != nil {
		nf.Log.Debug("Failed subscribing to notifications", zap.Error(err))
		return
	}

	select {
	case <-nf.closeChan:
		return
	default:
	}

	if err := nf.Engine.Notify(context.Background(), msg); err != nil {
		nf.Log.Debug("Failed notifying engine", zap.Error(err))
		return
	}

	select {
	case <-nf.closeChan:
		return
	case <-ctx.Done():
	}
}

func (nf *NotificationForwarder) PreferenceOrStateChanged() {
	nf.cancelContext()
}

func (nf *NotificationForwarder) cancelContext() {
	nf.lock.Lock()
	defer nf.lock.Unlock()

	if nf.closeFunc != nil {
		nf.closeFunc()
		nf.closeFunc = nil
	}
}

func (nf *NotificationForwarder) setAndGetContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	nf.lock.Lock()
	defer nf.lock.Unlock()
	nf.closeFunc = cancel
	return ctx
}

func (nf *NotificationForwarder) Close() {
	defer nf.running.Wait()

	select {
	case <-nf.closeChan:
	default:
		close(nf.closeChan)
		nf.cancelContext()
	}
}
