// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

type Scheduler interface {
	Dispatch(startTime time.Time)

	// Client must guarantee that [SetBuildBlockTime]
	// is never called after [Close]
	SetBuildBlockTime(t time.Time)
	Close()
}

// Scheduler receives notifications from a VM that it wants its engine to call
// the VM's BuildBlock method, and delivers the notification to the engine only
// when the engine should call BuildBlock. Namely, when this node is allowed to
// propose a block under the congestion control mechanism.
type scheduler struct {
	subscriptionDelayer *common.SubscriptionProxy

	// When we receive a message on this channel, it means that we must refrain
	// from telling the engine to call its VM's BuildBlock method until the
	// given time
	newBuildBlockTime chan time.Time
	onClose           context.CancelFunc
}

func New(subscription common.Subscription) (Scheduler, *common.SubscriptionProxy) {
	sp := common.NewSubscriptionProxy(subscription)
	return &scheduler{
		onClose:             sp.Close,
		subscriptionDelayer: sp,
		newBuildBlockTime:   make(chan time.Time),
	}, sp
}

func (s *scheduler) Dispatch(buildBlockTime time.Time) {
	timer := time.NewTimer(time.Until(buildBlockTime))
waitloop:
	for {
		select {
		case <-timer.C: // It's time to tell the engine to try to build a block
		case buildBlockTime, ok := <-s.newBuildBlockTime:
			// Stop the timer and clear [timer.C] if needed
			if !timer.Stop() {
				<-timer.C
			}

			if !ok {
				// s.Close() was called
				return
			}

			// The time at which we should notify the engine that it should try
			// to build a block has changed
			timer.Reset(time.Until(buildBlockTime))
			continue waitloop
		}

		for {
			ctx, cancel := context.WithCancel(context.Background())

			select {
			case <-s.subscriptionDelayer.Forward(ctx):
				cancel()
			case buildBlockTime, ok := <-s.newBuildBlockTime:
				cancel()
				// The time at which we should notify the engine that it should
				// try to build a block has changed
				if !ok {
					// s.Close() was called
					return
				}
				// We know [timer.C] was drained in the first select statement
				// so its safe to call [timer.Reset]
				timer.Reset(time.Until(buildBlockTime))
				continue waitloop
			}
		}
	}
}

func (s *scheduler) SetBuildBlockTime(t time.Time) {
	s.newBuildBlockTime <- t
}

func (s *scheduler) Close() {
	close(s.newBuildBlockTime)
	s.onClose()
}
