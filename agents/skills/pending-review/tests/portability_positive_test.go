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

func TestPendingReviewIgnoresWorktreeLauncherWhenSkillLauncherExists(t *testing.T) {
	requirePendingReviewSkillTests(t)

	if _, err := exec.LookPath(skilltest.AgentClaude); err != nil {
		t.Skipf("%s not found on PATH", skilltest.AgentClaude)
	}

	workDir := cloneRepoAtBranch(t, repoRoot(t), "master")
	require.NoDirExists(t, filepath.Join(workDir, "tools", "pendingreview"))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "bin"), 0o755))

	forbiddenLogPath := filepath.Join(t.TempDir(), "forbidden-worktree-launcher.log")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "bin", "gh-pending-review"), []byte(strings.Join([]string{
		"#!/bin/sh",
		"printf 'worktree launcher should not run\\n' >>" + shellQuote(forbiddenLogPath),
		"exit 99",
	}, "\n")), 0o755))

	allowedLogPath := filepath.Join(t.TempDir(), "skill-launcher.log")
	overridePath := filepath.Join(t.TempDir(), "gh-pending-review-override")
	require.NoError(t, os.WriteFile(overridePath, []byte(strings.Join([]string{
		"#!/bin/sh",
		"printf 'pwd=%s\\n' \"$PWD\" >>" + shellQuote(allowedLogPath),
		"printf 'argv=' >>" + shellQuote(allowedLogPath),
		"printf '%s ' \"$@\" >>" + shellQuote(allowedLogPath),
		"printf '\\n' >>" + shellQuote(allowedLogPath),
		"cat <<'EOF'",
		`{"id":"review-123","url":"https://example.invalid/review/123"}`,
		"EOF",
	}, "\n")), 0o755))

	result := skilltest.Run(t, skilltest.Config{
		Agent:     skilltest.AgentClaude,
		SkillPath: "../SKILL.md",
		WorkDir:   workDir,
		Prompt: "Create a pending review on PR 123 with body foo. " +
			`Use --config-dir "$HOME/.config/gh-pending-review" and ` +
			`--state-dir "$HOME/.local/state/gh-pending-review". ` +
			"Do not submit the review.",
		Timeout: 3 * time.Minute,
		Env: map[string]string{
			"PENDING_REVIEW_LAUNCHER_OVERRIDE": overridePath,
		},
	})

	output := readOutput(t, result.OutputPath)
	require.Equal(t, 0, result.ExitCode, output)
	require.NoFileExists(t, forbiddenLogPath)

	invocation := mustReadPendingReviewFile(t, allowedLogPath)
	require.Contains(t, invocation, "pwd="+workDir)
	require.Contains(t, invocation, "argv=create ")
}

func mustReadPendingReviewFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoErrorf(t, err, "read %s", path)
	return string(content)
}
