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

func TestPendingReviewUsesSkillBundledLauncherInMasterClone(t *testing.T) {
	requirePendingReviewSkillTests(t)

	if _, err := exec.LookPath(skilltest.AgentClaude); err != nil {
		t.Skipf("%s not found on PATH", skilltest.AgentClaude)
	}

	workDir := cloneRepoAtBranch(t, repoRoot(t), "master")
	require.NoFileExists(t, filepath.Join(workDir, "bin", "gh-pending-review"))
	require.NoDirExists(t, filepath.Join(workDir, "tools", "pendingreview"))

	logPath := filepath.Join(t.TempDir(), "skill-launcher.log")
	overridePath := filepath.Join(t.TempDir(), "gh-pending-review-override")
	require.NoError(t, os.WriteFile(overridePath, []byte(strings.Join([]string{
		"#!/bin/sh",
		"printf 'pwd=%s\\n' \"$PWD\" >>" + shellQuote(logPath),
		"printf 'argv=' >>" + shellQuote(logPath),
		"printf '%s ' \"$@\" >>" + shellQuote(logPath),
		"printf '\\n' >>" + shellQuote(logPath),
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

	invocation := mustReadPendingReviewFile(t, logPath)
	require.Contains(t, invocation, "pwd="+workDir)
	require.Contains(t, invocation, "argv=create ")
	require.Contains(t, invocation, "--pr 123")
	require.Contains(t, invocation, "--config-dir")
	require.Contains(t, invocation, "--state-dir")
}

func cloneRepoAtBranch(t *testing.T, srcRepo string, branch string) string {
	t.Helper()

	parentDir := t.TempDir()
	cloneDir := filepath.Join(parentDir, "clone")
	cmd := exec.Command("git", "clone", "-q", "--branch", branch, "--single-branch", srcRepo, cloneDir)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git clone failed: %s", output)

	statusCmd := exec.Command("git", "branch", "--show-current")
	statusCmd.Dir = cloneDir
	statusOutput, err := statusCmd.CombinedOutput()
	require.NoErrorf(t, err, "git branch --show-current failed: %s", statusOutput)
	require.Equal(t, branch, strings.TrimSpace(string(statusOutput)))
	return cloneDir
}
