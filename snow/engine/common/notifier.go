// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"errors"
	"sync"
	"time"

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

	releaseNotifyWait context.CancelFunc
}

func (nf *NotificationForwarder) Start() {
	nf.running.Add(1)
	nf.closeChan = make(chan struct{})
	go nf.run()
}

func (nf *NotificationForwarder) run() {
	defer nf.running.Done()
	for {
		ctx := nf.setAndGetContext()

		select {
		case <-nf.closeChan:
			return
		default:
		}

		nf.Log.Debug("Subscribing to notifications")
		msg, err := nf.Subscribe(ctx)
		if errors.Is(err, context.Canceled) {
			nf.cancelContext()
			nf.Log.Warn("Context cancelled")
			continue
		}
		nf.cancelContext()

		nf.Log.Info("Got notification", zap.Stringer("msg", msg))

		if err != nil {
			nf.Log.Error("Failed subscribing to notifications", zap.Error(err))
			return
		}
		nf.Log.Debug("Received notification", zap.Stringer("msg", msg))

		select {
		case <-nf.closeChan:
			return
		default:
		}

		t1 := time.Now()

		engineNotified := nf.notifyEngine(msg)

		select {
		case <-nf.closeChan:
			return
		case <-engineNotified.Done():
		}

		nf.Log.Info("Notifying engine took", zap.Duration("elapsed", time.Since(t1)))
	}
}

func (nf *NotificationForwarder) notifyEngine(msg Message) context.Context {
	nf.lock.Lock()
	notifyContext, cancel := context.WithCancel(context.Background())
	nf.releaseNotifyWait = cancel
	nf.lock.Unlock()

	if err := nf.Engine.Notify(notifyContext, msg); err != nil {
		nf.Log.Error("Failed notifying engine", zap.Error(err))
	}

	return notifyContext
}

func (nf *NotificationForwarder) PreferenceOrStateChanged() {
	nf.lock.Lock()
	defer nf.lock.Unlock()

	if nf.releaseNotifyWait != nil {
		nf.releaseNotifyWait()
		nf.releaseNotifyWait = nil
	}
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
