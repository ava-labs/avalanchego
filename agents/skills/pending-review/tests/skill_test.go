// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tools/skilltest"
)

func TestPendingReviewGetState(t *testing.T) {
	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			workDir := newPendingReviewTestRepo(t)
			logPath := installPendingReviewWrapper(t, workDir)

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   workDir,
				Prompt: "Inspect my local pending review state for PR 123 as github " +
					"login octocat. Do not change anything.",
				Timeout: 2 * time.Minute,
			})

			require.Equal(t, 0, result.ExitCode)

			invocation, err := os.ReadFile(logPath)
			require.NoError(t, err)
			got := string(invocation)
			require.Contains(t, got, "get-state")
			require.Contains(t, got, "--pr")
			require.Contains(t, got, "123")
			require.Contains(t, got, "--user")
			require.Contains(t, got, "octocat")
			require.Contains(t, got, "--state-dir")
			require.NotContains(t, got, "delete")
			require.NotContains(t, got, "update-body")
			require.NotContains(t, got, "replace-comments")
		})
	}
}

func newPendingReviewTestRepo(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "bin"), 0o755))
	require.NoError(t, exec.Command("git", "init", "-q", workDir).Run())
	return workDir
}

func installPendingReviewWrapper(t *testing.T, workDir string) string {
	t.Helper()

	logPath := filepath.Join(workDir, "gh-pending-review.log")
	script := strings.Join([]string{
		"#!/bin/sh",
		"printf '%s\\n' \"$@\" >" + shellQuote(logPath),
		"cat <<'EOF'",
		"{\"body\":\"Saved draft review body\",\"comments\":[]}",
		"EOF",
	}, "\n") + "\n"

	wrapperPath := filepath.Join(workDir, "bin", "gh-pending-review")
	require.NoError(t, os.WriteFile(wrapperPath, []byte(script), 0o755))
	return logPath
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
