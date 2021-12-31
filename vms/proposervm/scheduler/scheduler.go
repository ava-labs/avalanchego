// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type Scheduler interface {
	Dispatch(startTime time.Time)
	SetBuildBlockTime(t time.Time)
	Close()
}

// Scheduler receives notifications from a VM that it wants its engine to call
// the VM's BuildBlock method, and delivers the notification to the engine only
// when the engine should call BuildBlock. Namely, when this node is allowed to
// propose a block under the congestion control mechanism.
type scheduler struct {
	log logging.Logger
	// The VM sends a message on this channel when it wants to tell the engine
	// that the engine should call the VM's BuildBlock method
	fromVM <-chan common.Message
	// The scheduler sends a message on this channel to notify the engine that
	// it should call its VM's BuildBlock method
	toEngine chan<- common.Message
	// When we receive a message on this channel, it means that we must refrain
	// from telling the engine to call its VM's BuildBlock method until the
	// given time
	newBuildBlockTime chan time.Time
}

func New(log logging.Logger, toEngine chan<- common.Message) (Scheduler, chan<- common.Message) {
	vmToEngine := make(chan common.Message, cap(toEngine))
	return &scheduler{
		log:               log,
		fromVM:            vmToEngine,
		toEngine:          toEngine,
		newBuildBlockTime: make(chan time.Time),
	}, vmToEngine
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
			select {
			case msg := <-s.fromVM:
				// Give the engine the message from the VM asking the engine to
				// build a block
				select {
				case s.toEngine <- msg:
				default:
					// If the channel to the engine is full, drop the message
					// from the VM to avoid deadlock
					s.log.Debug("dropping message %s from VM because channel to engine is full", msg)
				}
			case buildBlockTime, ok := <-s.newBuildBlockTime:
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
}
