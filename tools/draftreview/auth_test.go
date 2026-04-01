// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package draftreview

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsolatedGitHubEnv(t *testing.T) {
	t.Parallel()

	env := isolatedGitHubEnv([]string{
		"PATH=/bin",
		"GH_TOKEN=ambient",
		"GITHUB_TOKEN=ambient",
		"GH_ENTERPRISE_TOKEN=ambient",
		"GITHUB_ENTERPRISE_TOKEN=ambient",
		"GH_CONFIG_DIR=/tmp/old",
		"KEEP=1",
	}, "/tmp/config")

	require.False(t, slicesContainPrefix(env, "GH_TOKEN="), "expected GH_TOKEN to be removed: %v", env)
	require.False(t, slicesContainPrefix(env, "GITHUB_TOKEN="), "expected GITHUB_TOKEN to be removed: %v", env)
	require.False(t, slicesContainPrefix(env, "GH_ENTERPRISE_TOKEN="), "expected GH_ENTERPRISE_TOKEN to be removed: %v", env)
	require.False(t, slicesContainPrefix(env, "GITHUB_ENTERPRISE_TOKEN="), "expected GITHUB_ENTERPRISE_TOKEN to be removed: %v", env)
	require.True(t, slicesContainExact(env, "GH_CONFIG_DIR=/tmp/config"), "expected GH_CONFIG_DIR override: %v", env)
	require.True(t, slicesContainExact(env, "GH_PROMPT_DISABLED=1"), "expected GH_PROMPT_DISABLED=1: %v", env)
	require.True(t, slicesContainExact(env, "KEEP=1"), "expected unrelated env to survive: %v", env)
}

func TestTokenProviderToken(t *testing.T) {
	t.Parallel()

	var gotName string
	var gotArgs []string
	var gotEnv []string

	provider := GHTokenProvider{
		env: []string{"PATH=/bin", "GH_TOKEN=ambient"},
		exec: func(_ context.Context, env []string, name string, args ...string) ([]byte, error) {
			gotName = name
			gotArgs = append([]string(nil), args...)
			gotEnv = append([]string(nil), env...)
			return []byte("token-value\n"), nil
		},
	}

	token, err := provider.Token(t.Context(), "/tmp/config")
	require.NoError(t, err)
	require.Equal(t, "token-value", token)
	require.Equal(t, "gh", gotName)
	wantArgs := []string{"auth", "token", "--hostname", "github.com"}
	require.True(t, reflect.DeepEqual(gotArgs, wantArgs), "unexpected args: got %v want %v", gotArgs, wantArgs)
	require.True(t, slicesContainExact(gotEnv, "GH_CONFIG_DIR=/tmp/config"), "expected isolated GH_CONFIG_DIR: %v", gotEnv)
	require.False(t, slicesContainPrefix(gotEnv, "GH_TOKEN="), "expected GH_TOKEN to be scrubbed: %v", gotEnv)
}

func slicesContainExact(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func slicesContainPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
