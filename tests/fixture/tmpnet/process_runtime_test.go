// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// newTestProcessNode returns a node whose process runtime runs the provided
// shell script in place of avalanchego.
func newTestProcessNode(t *testing.T, script string) *Node {
	t.Helper()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "avalanchego")
	require.NoError(t, os.WriteFile(binaryPath, []byte("#!/bin/sh\n"+script), perms.ReadWriteExecute))

	node := &Node{
		NodeID:  ids.GenerateTestNodeID(),
		Flags:   FlagsMap{},
		DataDir: filepath.Join(dir, "data"),
		RuntimeConfig: &NodeRuntimeConfig{
			Process: &ProcessRuntimeConfig{
				AvalancheGoPath: binaryPath,
			},
		},
	}
	node.network = &Network{
		log:   logging.NoLog{},
		Nodes: []*Node{node},
	}
	require.NoError(t, os.MkdirAll(node.DataDir, perms.ReadWriteExecute))
	return node
}

func TestProcessRuntimeStartReturnsWhenProcessExits(t *testing.T) {
	node := newTestProcessNode(t, "exit 1\n")

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	err := node.Start(ctx)
	require.ErrorIs(t, err, errProcessExited)
	require.NoError(t, ctx.Err(), "Start must return on process exit rather than waiting for the context")
}

func TestProcessRuntimeStartStopsProcessOnFailure(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pid")
	// The process records its PID and then runs without ever writing a
	// process context.
	node := newTestProcessNode(t, fmt.Sprintf("echo $$ > %q\nexec sleep 300\n", pidPath))

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	err := node.Start(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	pidBytes, err := os.ReadFile(pidPath)
	require.NoError(t, err, "process did not start before the context expired")
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, err)

	err = syscall.Kill(pid, syscall.Signal(0))
	if !errors.Is(err, syscall.ESRCH) {
		// Avoid leaking the process when the assertion below fails
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	require.ErrorIs(t, err, syscall.ESRCH, "process %d still running after failed start", pid)
}
