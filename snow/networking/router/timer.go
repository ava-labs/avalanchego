// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import "time"

type Timer struct {
	Handler *Handler
	Preempt chan struct{}
}

func (t *Timer) RegisterTimeout(d time.Duration) {
	go func() {
		timer := time.NewTimer(d)
		defer timer.Stop()

		tickerChan := timer.C
		closerChan := make(chan struct{})

		close(closerChan)

		select {
		case <-tickerChan:
		case <-closerChan:
		}

		t.Handler.Timeout()
	}()
}
