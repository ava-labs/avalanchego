// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"os/signal"
	"syscall"
	"time"
)

// DefaultNotifyContext returns a context that is marked done when signals indicating
// process termination are received. If a non-zero duration is provided, the parent to the
// notify context will be a context with a timeout for that duration.
func DefaultNotifyContext(duration time.Duration, cleanup func(func())) context.Context {
	parentContext := context.Background()
	if duration > 0 {
		var cancel context.CancelFunc
		parentContext, cancel = context.WithTimeout(parentContext, duration)
		cleanup(cancel)
	}
	ctx, stop := signal.NotifyContext(parentContext, syscall.SIGTERM, syscall.SIGINT)
	cleanup(stop)
	return ctx
}
