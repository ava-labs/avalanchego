// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		envBase   string
		wantCfg   config
		wantArgs  []string
		wantError string
	}{
		{
			name:     "explicit diff flag",
			args:     []string{"--diff", "origin/master..", "test", "//..."},
			wantCfg:  config{diffRange: "origin/master.."},
			wantArgs: []string{"test", "//..."},
		},
		{
			name:     "explicit policy flag",
			args:     []string{"--policy", "unit-go-test", "test", "//..."},
			wantCfg:  config{policy: "unit-go-test"},
			wantArgs: []string{"test", "//..."},
		},
		{
			name:     "diff from env base sha",
			args:     []string{"test", "//..."},
			envBase:  "abc1234",
			wantCfg:  config{diffRange: "abc1234.."},
			wantArgs: []string{"test", "//..."},
		},
		{
			name:      "missing diff value",
			args:      []string{"--diff"},
			wantError: "--diff requires a value",
		},
		{
			name:      "missing policy value",
			args:      []string{"--policy"},
			wantError: "--policy requires a value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BAZEL_IMPACTED_BASE_SHA", tt.envBase)
			t.Setenv("BAZEL_IMPACTED_DIFF_RANGE", "")

			gotCfg, gotArgs, err := parseArgs(tt.args)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantCfg, gotCfg)
			require.Equal(t, tt.wantArgs, gotArgs)
		})
	}
}

func TestParseTestInvocation(t *testing.T) {
	t.Parallel()

	t.Run("plain positive target patterns", func(t *testing.T) {
		invocation, err := parseTestInvocation([]string{"test", "--config=fast", "//..."})
		require.NoError(t, err)
		require.Equal(t, []string{"--config=fast"}, invocation.preTargetArgs)
		require.Equal(t, []string{"//..."}, invocation.targetPatterns)
		require.Empty(t, invocation.postDashArgs)
	})

	t.Run("negative target patterns after end of options marker", func(t *testing.T) {
		invocation, err := parseTestInvocation([]string{"test", "//...", "--", "-//graft/..."})
		require.NoError(t, err)
		require.Empty(t, invocation.preTargetArgs)
		require.Equal(t, []string{"//...", "-//graft/..."}, invocation.targetPatterns)
		require.Empty(t, invocation.postDashArgs)
	})

	t.Run("test args after second marker", func(t *testing.T) {
		invocation, err := parseTestInvocation([]string{"test", "//...", "--", "-//graft/...", "--", "--test_arg=value"})
		require.NoError(t, err)
		require.Equal(t, []string{"//...", "-//graft/..."}, invocation.targetPatterns)
		require.Equal(t, []string{"--test_arg=value"}, invocation.postDashArgs)
	})
}

func TestDiffRangeFromEnvPrefersExplicitRange(t *testing.T) {
	t.Setenv("BAZEL_IMPACTED_DIFF_RANGE", "origin/master..")
	t.Setenv("BAZEL_IMPACTED_BASE_SHA", "abc1234")
	require.Equal(t, "origin/master..", diffRangeFromEnv())
}

func TestImpactedTestsCommand(t *testing.T) {
	const repoRoot = "/tmp/repo"
	targetPatterns := []string{"//...", "-//graft/..."}
	wantSelectorArgs := []string{"select", "--range", "abc123..", "--command", "test", "--policy", policyAuto, "--scope", "//...", "--scope", "-//graft/..."}

	t.Run("defaults to go run", func(t *testing.T) {
		t.Setenv("IMPACTEDTESTS_BIN", "")

		cmd := impactedTestsCommand(repoRoot, "abc123..", "test", targetPatterns, policyAuto)
		require.Equal(t, append([]string{"go", "run", "/tmp/repo/tools/impactedtests"}, wantSelectorArgs...), cmd.Args)
	})

	t.Run("uses explicit binary when configured", func(t *testing.T) {
		t.Setenv("IMPACTEDTESTS_BIN", "/tmp/bin/impactedtests")

		cmd := impactedTestsCommand(repoRoot, "abc123..", "test", targetPatterns, policyAuto)
		require.Equal(t, append([]string{"/tmp/bin/impactedtests"}, wantSelectorArgs...), cmd.Args)
	})
}

func TestIsTargetPattern(t *testing.T) {
	t.Parallel()

	require.True(t, isTargetPattern("//..."))
	require.True(t, isTargetPattern("-//graft/..."))
	require.True(t, isTargetPattern("@repo//pkg:target"))
	require.False(t, isTargetPattern("--config=fast"))
	require.False(t, isTargetPattern("test"))
}
