// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tools/skilltest"
)

const runReviewBriefSkillTestsEnv = "RUN_REVIEW_BRIEF_SKILL_TESTS"

func requireReviewBriefSkillTests(t *testing.T) {
	t.Helper()

	if os.Getenv(runReviewBriefSkillTestsEnv) != "1" {
		t.Skipf("set %s=1 to run agent-driven review-brief skill tests", runReviewBriefSkillTestsEnv)
	}
}

func TestReviewBriefUsesCurrentWorktreePaths(t *testing.T) {
	requireReviewBriefSkillTests(t)

	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			gitPath, err := exec.LookPath("git")
			require.NoError(t, err)

			workDir := newReviewBriefTestRepo(t, false)
			logPath := filepath.Join(t.TempDir(), "git.log")
			targetBrief := filepath.Join(workDir, ".review-briefs", "widget-change.md")
			sourceBrief := filepath.Join(repoRoot(t), ".review-briefs", "widget-change.md")
			require.NoFileExists(t, sourceBrief)

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   workDir,
				Prompt:    "Let's create a review brief at .review-briefs/widget-change.md for this change.",
				Timeout:   3 * time.Minute,
				BinWrappers: map[string]string{
					"git": gitLoggingWrapper(logPath, gitPath),
				},
			})

			output := readOutput(t, result.OutputPath)
			require.Equal(t, 0, result.ExitCode, output)
			require.FileExists(t, targetBrief)
			require.NoFileExists(t, sourceBrief)

			brief := mustReadFile(t, targetBrief)
			require.NotEmpty(t, strings.TrimSpace(brief))
			require.Contains(t, strings.ToLower(brief), "overview")
			require.Contains(t, brief, "widget.txt")

			gitLog := mustReadFile(t, logPath)
			require.Contains(t, gitLog, "pwd="+workDir)
		})
	}
}

func TestReviewBriefUsesBundledConventionWhenRepoHasNone(t *testing.T) {
	requireReviewBriefSkillTests(t)

	for _, agent := range []string{skilltest.AgentClaude, skilltest.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skipf("%s not found on PATH", agent)
			}

			workDir := newReviewBriefTestRepo(t, false)
			targetBrief := filepath.Join(workDir, ".review-briefs", "missing-convention.md")
			sourceBrief := filepath.Join(repoRoot(t), ".review-briefs", "missing-convention.md")
			require.NoFileExists(t, sourceBrief)

			result := skilltest.Run(t, skilltest.Config{
				Agent:     agent,
				SkillPath: "../SKILL.md",
				WorkDir:   workDir,
				Prompt:    "Let's create a review brief at .review-briefs/missing-convention.md for this change.",
				Timeout:   3 * time.Minute,
			})

			output := readOutput(t, result.OutputPath)
			require.Equal(t, 0, result.ExitCode, output)
			require.FileExists(t, targetBrief)
			require.NoFileExists(t, sourceBrief)
			brief := mustReadFile(t, targetBrief)
			require.Contains(t, strings.ToLower(brief), "overview")
			require.Contains(t, brief, "widget.txt")
		})
	}
}

func newReviewBriefTestRepo(t *testing.T, withConvention bool) string {
	t.Helper()

	workDir := t.TempDir()
	runGit(t, workDir, "init", "-q")
	runGit(t, workDir, "config", "user.name", "Skill Test")
	runGit(t, workDir, "config", "user.email", "skill-test@example.com")

	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "pkg"), 0o755))
	widgetPath := filepath.Join(workDir, "pkg", "widget.txt")
	require.NoError(t, os.WriteFile(widgetPath, []byte("before\n"), 0o644))

	if withConvention {
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, ".review-briefs"), 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(workDir, ".review-briefs", "README.md"),
			[]byte(strings.TrimSpace(`# PR review briefs

Use this directory for PR-scoped review companions in this test repo.
Always keep the brief aligned with the current diff.
`)+"\n"),
			0o644,
		))
	}

	runGit(t, workDir, "add", ".")
	runGit(t, workDir, "commit", "-qm", "initial")

	require.NoError(t, os.WriteFile(widgetPath, []byte("after\n"), 0o644))
	return workDir
}

func runGit(t *testing.T, workDir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s failed: %s", strings.Join(args, " "), output)
}

func gitLoggingWrapper(logPath string, gitPath string) string {
	return strings.Join([]string{
		"#!/bin/sh",
		"printf 'pwd=%s\\n' \"$PWD\" >>" + shellQuote(logPath),
		"printf 'args=' >>" + shellQuote(logPath),
		"printf '%s ' \"$@\" >>" + shellQuote(logPath),
		"printf '\\n' >>" + shellQuote(logPath),
		"exec " + shellQuote(gitPath) + " \"$@\"",
	}, "\n") + "\n"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", ".."))
}

func readOutput(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoErrorf(t, err, "read %s", path)
	return string(content)
}

func TestReviewBriefTestRepoHasMeaningfulDiff(t *testing.T) {
	workDir := newReviewBriefTestRepo(t, true)
	cmd := exec.Command("git", "diff", "--stat")
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(output), "pkg/widget.txt")
	require.Contains(t, string(output), fmt.Sprintf("%s |", filepath.ToSlash(filepath.Join("pkg", "widget.txt"))))
}
