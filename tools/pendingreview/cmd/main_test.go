// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tools/pendingreview"
)

func TestMainPrintsStackTraceWhenEnabled(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Args = []string{"gh-pending-review"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMainPrintsStackTraceWhenEnabled")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"STACK_TRACE_ERRORS=1",
	)

	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)

	stderr := string(output)
	require.Contains(t, stderr, "missing command")
	require.Contains(t, stderr, "Stack trace:")
	require.Contains(t, stderr, "/tools/pendingreview/cmd/main.go:")
}

func TestRunServeProxyRejectsUnexpectedArgs(t *testing.T) {
	var stdout bytes.Buffer

	err := run(context.Background(), []string{"serve-proxy", "extra"}, bytes.NewReader(nil), &stdout, &stdout)
	require.EqualError(t, err, "unexpected trailing arguments: [extra]\n\n"+pendingreview.Usage())
}
