// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"
	"time"
)

// TimerTest is a test timer
type TimerTest struct {
	T *testing.T

	CantRegisterTimout bool

	RegisterTimeoutF func(time.Duration, chan struct{})
}

// Default set the default callable value to [cant]
func (t *TimerTest) Default(cant bool) {
	t.CantRegisterTimout = cant
}

func (t *TimerTest) RegisterTimeout(delay time.Duration, preempt chan struct{}) {
	if t.RegisterTimeoutF != nil {
		t.RegisterTimeoutF(delay, preempt)
	} else if t.CantRegisterTimout && t.T != nil {
		t.T.Fatalf("Unexpectedly called RegisterTimeout")
	}
}
