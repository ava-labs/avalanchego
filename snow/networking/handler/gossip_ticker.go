// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import "time"

// gossipTicker mirrors the shape of *time.Ticker, but a [frequency] of 0
// produces a disabled ticker whose channel never fires.
type gossipTicker struct {
	C    <-chan time.Time
	stop func()
}

// Stop releases the underlying ticker, if any.
func (g *gossipTicker) Stop() {
	g.stop()
}

// newGossipTicker returns a ticker that fires every [frequency]. If
// [frequency] is 0, the returned ticker is disabled.
func newGossipTicker(frequency time.Duration) *gossipTicker {
	if frequency <= 0 {
		return &gossipTicker{stop: func() {}}
	}
	t := time.NewTicker(frequency)
	return &gossipTicker{
		C:    t.C,
		stop: t.Stop,
	}
}
