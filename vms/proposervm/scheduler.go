package proposervm

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// scheduler control the signal dispatching from coreVM to consensus engine

type scheduler struct {
	vm             *VM
	fromCoreVM     chan common.Message
	toEngine       chan<- common.Message
	newAcceptedBlk chan time.Time
	availBlks      []common.Message
	blkTicker      *time.Ticker
	blkWinStart    time.Time
}

func (s *scheduler) initialize(vm *VM, toEngine chan<- common.Message) error {
	s.vm = vm
	s.toEngine = toEngine
	s.fromCoreVM = make(chan common.Message, len(toEngine))
	s.newAcceptedBlk = make(chan time.Time, 1024)
	s.availBlks = make([]common.Message, 0)
	return nil
}

func (s *scheduler) coreVMChannel() chan<- common.Message { return s.fromCoreVM }

func (s *scheduler) rescheduleBlkTicker() {
	winStart := s.blkWinStart
	var tickerDelay time.Duration
	tNow := s.vm.clock.now()
	if winStart.After(tNow) {
		tickerDelay = winStart.Sub(tNow)
	} else {
		tickerDelay = time.Millisecond
	}
	s.blkTicker = time.NewTicker(tickerDelay)
}

func (s *scheduler) handleBlockTiming() {
	for {
		select {
		case msg := <-s.fromCoreVM:
			s.availBlks = append(s.availBlks, msg)
		case <-s.blkTicker.C:
			if len(s.availBlks) > 0 {
				msg := s.availBlks[0]
				s.availBlks = s.availBlks[1:]
				s.toEngine <- msg
			}
		case s.blkWinStart = <-s.newAcceptedBlk:
			s.rescheduleBlkTicker()
		}
	}
}
