// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
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
