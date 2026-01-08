// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// WaitGroupWithTimeout waits for wg with timeout, failing the test with msg on timeout.
func WaitGroupWithTimeout(t *testing.T, wg *sync.WaitGroup, timeout time.Duration, msg string) {
	t.Helper()
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
		return
	case <-time.After(timeout):
		require.FailNow(t, msg)
	}
}

// WaitErrWithTimeout waits to receive an error from ch or fails on timeout.
func WaitErrWithTimeout(t *testing.T, ch <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for RunSyncerTasks to complete")
		return nil
	}
}

// WaitSignalWithTimeout waits for a signal from ch or fails on timeout with msg.
func WaitSignalWithTimeout(t *testing.T, ch <-chan struct{}, timeout time.Duration, msg string) {
	t.Helper()
	select {
	case <-ch:
		return
	case <-time.After(timeout):
		require.FailNow(t, msg)
	}
}
