// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	fromVMSize = 1024
)

type Scheduler interface {
	Dispatch(startTime time.Time)
	SetBuildBlockTime(t time.Time)
	Close()
}

// Scheduler receives notifications from a VM that it wants its engine to
// call the VM's BuildBlock method, and delivers the notification to the
// engine only when the engine should call BuildBlock. Namely, when this node is
// allowed to propose a block under the congestion control mechanism.
type scheduler struct {
	log logging.Logger
	// TODO this isn't used. Do we need this?
	activationTime time.Time
	// The VM sends a message on this channel when it wants to tell the engine
	// that the engine should call the VM's BuildBlock method
	fromVM <-chan common.Message
	// The scheduler sends a message on this channel to notify the engine that
	// it should call its VM's BuildBlock method
	toEngine chan<- common.Message
	// When we receive a message on this channel, it means that we must refrain
	// from telling the engine to call its VM's BuildBlock method until the given time
	newBuildBlockTime chan time.Time
}

func New(log logging.Logger, toEngine chan<- common.Message, activationTime time.Time) (Scheduler, chan<- common.Message) {
	vmToEngine := make(chan common.Message, fromVMSize)
	return &scheduler{
		activationTime:    activationTime,
		fromVM:            vmToEngine,
		toEngine:          toEngine,
		newBuildBlockTime: make(chan time.Time),
	}, vmToEngine
}

func (s *scheduler) Dispatch(buildBlockTime time.Time) {
	timer := time.NewTimer(time.Until(buildBlockTime))
	var ok bool
waitloop:
	for {
		select {
		case <-timer.C: // It's time to tell the engine to try to build a block
		case buildBlockTime, ok = <-s.newBuildBlockTime:
			if !ok {
				// s.Close() was called
				timer.Stop()
				return
			}
			// The time at which we should notify the engine that
			// it should try to build a block has changed
			timer.Reset(time.Until(buildBlockTime))
			continue waitloop
		}

		for {
			select {
			case msg := <-s.fromVM:
				// Give the engine the message from the VM asking the engine to build a block
				select {
				case s.toEngine <- msg:
				default:
					// If the channel to the engine is full, drop the message from the VM to avoid deadlock
					s.log.Debug("dropping message from VM because channel to engine is full")
				}
			case buildBlockTime, ok = <-s.newBuildBlockTime:
				// The time at which we should notify the engine that
				// it should try to build a block has changed
				if !ok {
					// s.Close() was called
					return
				}
				// We know [timer.C] was drained in the first select
				// statement so its safe to call [timer.Reset]
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
