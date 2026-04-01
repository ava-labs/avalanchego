package main

import (
	"os"
	"os/exec"
	"strings"
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
	require.Error(t, err)

	stderr := string(output)
	require.True(t, strings.Contains(stderr, "missing command"), "expected usage error in output, got %q", stderr)
	require.True(t, strings.Contains(stderr, "Stack trace:"), "expected stack trace in output, got %q", stderr)
	require.True(t, strings.Contains(stderr, "/tools/draftreview/cmd/main.go:"), "expected stack frame for main.go in output, got %q", stderr)
}
