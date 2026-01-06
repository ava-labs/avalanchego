// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import "time"

// StoppedTimer returns a stopped timer so that there is no entry on
// the C channel (and there isn't one scheduled to be added).
//
// This means that after calling Reset there will be no events on the
// channel until the timer fires (at which point there will be exactly
// one event sent to the channel).
//
// It enables re-using the timer across loop iterations without
// needing to have the first loop iteration perform any == nil checks
// to initialize the first invocation.
func StoppedTimer() *time.Timer {
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	return timer
}
