// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"os"
	"os/signal"
)

// HandleSignals calls f if the go runtime receives the any of the provided
// signals with the received signal.
//
// If f is nil or there are no provided signals, then nil will be returned.
// Otherwise, a signal channel will be returned that can be used to clear the
// signals registered by this function by valling ClearSignals.
func HandleSignals(f func(os.Signal), sigs ...os.Signal) chan<- os.Signal {
	if f == nil || len(sigs) == 0 {
		return nil
	}

	// register signals
	c := make(chan os.Signal, 1)
	for _, sig := range sigs {
		signal.Notify(c, sig)
	}

	go func() {
		for sig := range c {
			f(sig)
		}
	}()

	return c
}

// ClearSignals clears any signals that have been registered on the provided
// channel and closes the channel.
func ClearSignals(c chan<- os.Signal) {
	if c == nil {
		return
	}

	signal.Stop(c)
	close(c)
}
