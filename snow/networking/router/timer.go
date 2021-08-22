// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ common.Timer = &Timer{}

type Timer struct {
	Handler *Handler
	Preempt chan struct{}
}

func (t *Timer) RegisterTimeout(d time.Duration) {
	go func() {
		timer := time.NewTimer(d)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-t.Preempt:
		}

		t.Handler.Timeout()
	}()
}
