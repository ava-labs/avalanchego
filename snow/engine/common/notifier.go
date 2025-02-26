// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
	"sync"
)

type Notifier interface {
	Notify(context.Context, Message) error
}

type NotificationForwarder struct {
	closeChan chan struct{}
	Notifier  Notifier
	Subscribe Subscription
	Log       logging.Logger

	lock   sync.Mutex
	cancel context.CancelFunc
}

func (nf *NotificationForwarder) Start() {
	nf.closeChan = make(chan struct{})
	go nf.run()
}

func (nf *NotificationForwarder) run() {
	for {
		select {
		case <-nf.closeChan:
			return
		default:
		}

		ctx := nf.getContext()
		msg := nf.Subscribe(ctx)
		nf.cancel()

		if err := nf.Notifier.Notify(ctx, msg); err != nil {
			nf.Log.Error("Failed notifying engine", zap.Error(err))
		}
	}
}

func (nf *NotificationForwarder) getContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	nf.lock.Lock()
	nf.cancel = cancel
	nf.lock.Unlock()
	return ctx
}

func (nf *NotificationForwarder) Close() {
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
