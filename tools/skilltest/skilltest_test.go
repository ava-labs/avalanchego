// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package skilltest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveModel_Default(t *testing.T) {
	cfg := Config{}
	require.Empty(t, resolveModel(cfg))
}

func TestResolveModel_ConfigOverride(t *testing.T) {
	cfg := Config{Model: "sonnet"}
	require.Equal(t, "sonnet", resolveModel(cfg))
}

func TestResolveModel_EnvOverride(t *testing.T) {
	t.Setenv("SKILL_TEST_MODEL", "opus")
	cfg := Config{Model: "sonnet"}
	require.Equal(t, "opus", resolveModel(cfg))
}

func TestResolveModel_AgentEnvOverride(t *testing.T) {
	t.Setenv("SKILL_TEST_MODEL_CODEX", "gpt-5.4")
	t.Setenv("SKILL_TEST_MODEL", "haiku")
	cfg := Config{Agent: AgentCodex, Model: "gpt-4.1"}
	require.Equal(t, "gpt-5.4", resolveModel(cfg))
}

func TestResolveAgent_Default(t *testing.T) {
	cfg := Config{}
	require.Equal(t, AgentClaude, resolveAgent(cfg))
}

func TestReadSkillFile_Absolute(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	expected := "# Test Skill\nDo the thing.\n"
	require.NoError(t, os.WriteFile(skillPath, []byte(expected), 0o644))

	content, err := readSkillFile(skillPath)
	require.NoError(t, err)
	require.Equal(t, expected, content)
}

func TestReadSkillFile_Missing(t *testing.T) {
	_, err := readSkillFile("/nonexistent/SKILL.md")
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.Contains(t, err.Error(), "reading skill file")
}

// TestReadSkillFile_Relative tests relative path resolution.
// readSkillFile uses runtime.Caller(3) to find the caller's file,
// so we simulate the real call depth: test -> depth1 -> depth2 -> readSkillFile.
func TestReadSkillFile_Relative(t *testing.T) {
	// This test file lives in tools/skilltest/, so "../../go.mod" should
	// resolve to the repo root go.mod.
	content, err := readSkillFileAtDepth3("../../go.mod")
	require.NoError(t, err)
	require.Contains(t, content, "module github.com/ava-labs/avalanchego")
}

// readSkillFileAtDepth3 wraps readSkillFile with two intermediate
// calls to match the runtime.Caller(3) skip count used in production:
// caller(0)=readSkillFile, caller(1)=depth2, caller(2)=depth1, caller(3)=test.
//
//go:noinline
func readSkillFileAtDepth3(path string) (string, error) {
	return readSkillFileDepth2(path)
}

//go:noinline
func readSkillFileDepth2(path string) (string, error) {
	return readSkillFile(path)
}

func TestFilteredEnv_RemovesClaudeVars(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("SKILLTEST_CANARY", "present")

	env := filteredEnv()

	envMap := make(map[string]string)
	for _, e := range env {
		idx := strings.Index(e, "=")
		if idx >= 0 {
			envMap[e[:idx]] = e[idx+1:]
		}
	}

	require.NotContains(t, envMap, "CLAUDECODE")
	require.NotContains(t, envMap, "CLAUDE_CODE_ENTRYPOINT")
	require.Equal(t, "present", envMap["SKILLTEST_CANARY"],
		"non-Claude env vars should be preserved")
}

func TestWrapPromptWithSkill(t *testing.T) {
	got := wrapPromptWithSkill("# Skill\nDo X.\n", "Handle request")
	require.Contains(t, got, "# Skill\nDo X.\n")
	require.Contains(t, got, "User request:\nHandle request")
}
