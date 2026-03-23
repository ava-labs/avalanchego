// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package earlysignal registers a SIGTERM handler during init() to prevent
// Go's default behavior of exiting with code 2 when SIGTERM arrives before
// the application's signal handler is registered in [app.Run].
//
// This is needed because compile-time instrumentation (e.g. Antithesis) can
// inject init() functions that run before main(), creating a window where
// SIGTERM has no handler. Import this package to close that window.
package earlysignal

import (
	"os"
	"os/signal"
	"syscall"
)

var ch = make(chan os.Signal, 1)

func init() {
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		<-ch
		os.Exit(0)
	}()
}

// Disable stops the early SIGTERM handler. This must be called before
// registering a new handler to ensure signals are not delivered to both
// channels. After calling Disable, the caller is responsible for handling
// SIGTERM.
func Disable() {
	signal.Stop(ch)
}
