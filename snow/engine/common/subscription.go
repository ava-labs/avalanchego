// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"
)

type Subscriber interface {
	// SubscribeToEvents blocks until either the given context is cancelled, or a message is returned.
	// The given pChainHeight is the height of the P-chain at the time of subscription.
	// The returned uint64 corresponds to the P-chain height at the time of returning the message.
	// The caller is expected to propagate to subsequent calls the P-chain height returned and not the one passed in.
	SubscribeToEvents(ctx context.Context) Message
}

// Subscription is a function that blocks until either the given context is cancelled, or a message is returned.
// The given pChainHeight is the height of the P-chain at the time of subscription.
// The returned uint64 corresponds to the P-chain height at the time of returning the message.
// The caller is expected to propagate to subsequent calls the P-chain height returned and not the one passed in.
type Subscription func(ctx context.Context) Message

type SimpleSubscriber struct {
	lock   sync.Mutex
	signal sync.Cond

	msg    *Message
	closed bool
}

func NewSimpleSubscriber() *SimpleSubscriber {
	ss := &SimpleSubscriber{}
	ss.signal = *sync.NewCond(&ss.lock)
	return ss
}

func (ss *SimpleSubscriber) Publish(msg Message) {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	ss.msg = &msg
	ss.signal.Broadcast()
}

func (ss *SimpleSubscriber) Close() {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	ss.closed = true
	ss.signal.Broadcast()
}

func (ss *SimpleSubscriber) SubscribeToEvents(ctx context.Context) Message {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		ss.signal.Broadcast()
	}()

	for {
		if ss.msg != nil {
			msg := *ss.msg
			ss.msg = nil
			return msg
		}

		select {
		case <-ctx.Done():
			return 0
		default:
			ss.signal.Wait()
		}
	}
}

type messageHeight struct {
	message Message
}

type SubscriptionDelayer struct {
	lock    sync.Mutex
	signal  sync.Cond
	running sync.WaitGroup

	absorbedMsgHeight *messageHeight
	releasedMsgHeight *messageHeight

	closed    bool
	subscribe Subscription
	onClose   context.CancelFunc
}

func NewSubscriptionDelayer(s Subscription) *SubscriptionDelayer {
	sd := &SubscriptionDelayer{
		subscribe: s,
	}

	sd.signal = *sync.NewCond(&sd.lock)

	sd.running.Add(1)
	go func() {
		defer sd.running.Done()
		sd.forward()
	}()

	return sd
}

func (sd *SubscriptionDelayer) Close() {
	defer sd.running.Wait()

	sd.lock.Lock()
	defer sd.lock.Unlock()

	if sd.onClose != nil {
		sd.onClose()
	}

	sd.closed = true
	sd.onClose = nil
}

func (sd *SubscriptionDelayer) isClosed() bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	return sd.closed
}

func (sd *SubscriptionDelayer) forward() {
	for !sd.isClosed() {
		ctx := sd.createContext()
		if sd.isClosed() {
			return
		}
		msg := sd.subscribe(ctx)
		if sd.isClosed() {
			return
		}
		sd.SetAbsorbedMsgAndHeight(msg)
		sd.signal.Broadcast()
	}
}

func (sd *SubscriptionDelayer) Absorb(ctx context.Context) {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		sd.signal.Broadcast()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if sd.absorbedMsgHeight != nil {
			return
		}

		sd.signal.Wait()
	}

}

func (sd *SubscriptionDelayer) Release() {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	if sd.absorbedMsgHeight != nil {
		sd.releasedMsgHeight = sd.absorbedMsgHeight
		sd.absorbedMsgHeight = nil
	}

	sd.signal.Broadcast()
}

func (sd *SubscriptionDelayer) SetAbsorbedMsgAndHeight(msg Message) {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	sd.absorbedMsgHeight = &messageHeight{
		message: msg,
	}
}

func (sd *SubscriptionDelayer) createContext() context.Context {
	sd.lock.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	sd.onClose = cancel
	sd.lock.Unlock()
	return ctx
}

func (sd *SubscriptionDelayer) SubscribeToEvents(ctx context.Context) Message {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		sd.signal.Broadcast()
	}()

	for {
		if sd.closed {
			return 0
		}

		if sd.releasedMsgHeight != nil {
			releasedMsgHeight := *sd.releasedMsgHeight
			sd.releasedMsgHeight = nil
			return releasedMsgHeight.message
		}

		select {
		case <-ctx.Done():
			return 0
		default:
			sd.signal.Wait()
		}
	}
}
