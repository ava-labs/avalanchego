// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"context"
	"sync"
	"testing"
	"time"
)

func NewTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	if d, ok := t.Deadline(); ok {
		return context.WithDeadline(context.Background(), d)
	}
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func WaitGroupWithContext(t *testing.T, ctx context.Context, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		// include context error for easier debugging
		t.Fatalf("timeout waiting for response: %v", ctx.Err())
	case <-done:
	}
}

func SleepWithContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		return
	}
}
