// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import "time"

// NewStagedTimer returns a timer that will execute [f]
// when a timeout occurs and execute an additional timeout after
// the returned duration if [f] returns true and some duration.
func NewStagedTimer(f func() (time.Duration, bool)) *Timer {
	t := NewTimer(nil)
	t.handler = func() {
		delay, repeat := f()
		if repeat {
			t.SetTimeoutIn(delay)
		}
	}
	return t
}
