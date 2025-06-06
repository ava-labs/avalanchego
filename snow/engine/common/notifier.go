// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
	"sync"
)

type Notifier interface {
	Notify(context.Context, Message) error
}

type NotificationForwarder struct {
	closeChan              chan struct{}
	BootstrappingOrSyncing bool
	GetPreference          func() ids.ID
	Notifier               Notifier
	Subscribe              Subscription
	Log                    logging.Logger

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

		prefBeforeSubscribing := nf.GetPreference()
		// We ignore the returned P-chain height because we check if the preference changed.
		// The P-chain height can only advance if the chain's preference has changed.
		msg, _ := nf.Subscribe(ctx, 0)
		prefAfterSubscribing := nf.GetPreference()

		// If we're bootstrapping or syncing, the notification doesn't end up in a block being built,
		// so it matters not if the preference changes during subscription.
		performingConsensus := !nf.BootstrappingOrSyncing

		// If our preference changed during the subscription, we should not notify the engine,
		// as building a block may now fail.
		preferenceChanged := prefBeforeSubscribing.Compare(prefAfterSubscribing) != 0

		if performingConsensus && preferenceChanged {
			nf.Log.Debug("Preference changed during subscription",
				zap.Stringer("before", prefBeforeSubscribing),
				zap.Stringer("after", prefAfterSubscribing),
			)
			continue
		}

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
