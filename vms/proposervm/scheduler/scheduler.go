// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

type Scheduler interface {
	Dispatch()
	SetStartTime(t time.Time)
	Close()
}

// scheduler control the signal dispatching from coreVM to consensus engine
type scheduler struct {
	fromVM       <-chan common.Message
	toEngine     chan<- common.Message
	newStartTime chan time.Time
	startTime    time.Time
}

func New(toEngine chan<- common.Message, startTime time.Time) (Scheduler, chan<- common.Message) {
	vmToEngine := make(chan common.Message, 1024)
	return &scheduler{
		fromVM:       vmToEngine,
		toEngine:     toEngine,
		newStartTime: make(chan time.Time),
		startTime:    startTime,
	}, vmToEngine
}

func (s *scheduler) Dispatch() {
waitloop:
	for {
		timer := time.NewTimer(time.Until(s.startTime))
		select {
		case <-timer.C:
		case startTime, ok := <-s.newStartTime:
			if !ok {
				timer.Stop()
				return
			}
			s.startTime = startTime
			timer.Stop()
			continue waitloop
		}

		for {
			select {
			case msg := <-s.fromVM:
				select {
				case s.toEngine <- msg:
				default:
				}
			case startTime, ok := <-s.newStartTime:
				if !ok {
					return
				}
				s.startTime = startTime
				continue waitloop
			}
		}
	}
}

func (s *scheduler) SetStartTime(t time.Time) {
	s.newStartTime <- t
}

func (s *scheduler) Close() {
	close(s.newStartTime)
}
