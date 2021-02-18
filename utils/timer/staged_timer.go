// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import "time"

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
