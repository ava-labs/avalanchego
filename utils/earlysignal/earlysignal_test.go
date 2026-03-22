// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package earlysignal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

const helperEnvVar = "EARLYSIGNAL_TEST_HELPER"

// TestEarlySIGTERM reproduces the bug where SIGTERM arriving before app.Run's
// signal.Notify causes Go to exit with code 2. The subprocess has
// earlysignal's init() active (same package), so the early handler should
// intercept SIGTERM and exit 0. We repeat this 50 times to stress-test the
// race window.
func TestEarlySIGTERM(t *testing.T) {
	require := require.New(t)

	for i := range 50 {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
		cmd.Env = append(os.Environ(), helperEnvVar+"=1")

		stdout, err := cmd.StdoutPipe()
		require.NoError(err, "iteration %d", i)

		require.NoError(cmd.Start(), "iteration %d", i)

		scanner := bufio.NewScanner(stdout)
		require.True(scanner.Scan(), "iteration %d: expected READY line", i)
		require.Equal("READY", scanner.Text(), "iteration %d", i)

		require.NoError(cmd.Process.Signal(syscall.SIGTERM), "iteration %d", i)

		err = cmd.Wait()
		require.NoError(err, "iteration %d: expected exit code 0, got %v", i, err)
	}
}

// TestDisableRestoresDefault verifies that after Disable() is called, SIGTERM
// causes a non-zero exit (Go's default code 2). This confirms the early
// handler is properly removed so app.Run's handler can take over.
func TestDisableRestoresDefault(t *testing.T) {
	require := require.New(t)

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessDisabled")
	cmd.Env = append(os.Environ(), helperEnvVar+"=1")

	stdout, err := cmd.StdoutPipe()
	require.NoError(err)

	require.NoError(cmd.Start())

	scanner := bufio.NewScanner(stdout)
	require.True(scanner.Scan(), "expected READY line")
	require.Equal("READY", scanner.Text())

	require.NoError(cmd.Process.Signal(syscall.SIGTERM))

	err = cmd.Wait()
	require.Error(err, "after Disable(), SIGTERM should cause non-zero exit")
}

// TestHelperProcess is the subprocess entry point for TestEarlySIGTERM.
// It is only executed when spawned as a subprocess with the helper env var set.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnvVar) != "1" {
		return
	}
	// earlysignal.init() has already run — signal handler is active.
	fmt.Println("READY")
	select {}
}

// TestHelperProcessDisabled is the subprocess entry point for
// TestDisableRestoresDefault. It calls Disable() to remove the early handler.
func TestHelperProcessDisabled(t *testing.T) {
	if os.Getenv(helperEnvVar) != "1" {
		return
	}
	Disable()
	fmt.Println("READY")
	select {}
}
