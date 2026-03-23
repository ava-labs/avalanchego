// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package earlysignal_test

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/app/earlysignal"
)

// TestEarlySIGTERM verifies that a SIGTERM received before main() begins
// results in a clean exit (code 0) rather than Go's default exit code 2.
//
// The test binary is re-executed as a subprocess with a sentinel env var.
// The subprocess imports earlysignal (via this test package) and then
// blocks in an init-like sleep, simulating slow init() functions.
func TestEarlySIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM is not supported on Windows")
	}

	const envKey = "EARLY_SIGNAL_TEST_SUBPROCESS"
	if os.Getenv(envKey) == "1" {
		// Subprocess path: block to simulate being stuck in init.
		// The early signal handler should catch SIGTERM and exit 0.
		time.Sleep(10 * time.Second)
		return
	}

	// Parent: re-run this test binary as a subprocess.
	cmd := exec.Command(os.Args[0], "-test.run=^TestEarlySIGTERM$")
	cmd.Env = append(os.Environ(), envKey+"=1")
	require.NoError(t, cmd.Start())

	// Give the subprocess time to start and register the init handler.
	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM — the early handler should catch it and exit 0.
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))

	err := cmd.Wait()
	require.NoError(t, err, "expected clean exit (code 0) on early SIGTERM, got: %v", err)
}

// TestSIGTERMAfterDisable verifies that after Disable() is called, SIGTERM
// is no longer caught by the early handler and kills the subprocess with
// the default signal disposition (non-zero exit).
func TestSIGTERMAfterDisable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM is not supported on Windows")
	}

	const envKey = "EARLY_SIGNAL_DISABLE_TEST_SUBPROCESS"
	if os.Getenv(envKey) == "1" {
		// Subprocess path: disable the early handler, then block.
		// SIGTERM should now use Go's default disposition (exit 2).
		earlysignal.Disable()
		time.Sleep(10 * time.Second)
		return
	}
Wher
	// Parent: re-run this test binary as a subprocess.
	cmd := exec.Command(os.Args[0], "-test.run=^TestSIGTERMAfterDisable$")
	cmd.Env = append(os.Environ(), envKey+"=1")
	require.NoError(t, cmd.Start())

	time.Sleep(100 * time.Millisecond)

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))

	err := cmd.Wait()
	require.Error(t, err, "expected non-zero exit after Disable(), but got clean exit")
}
