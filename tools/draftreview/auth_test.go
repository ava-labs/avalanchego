package draftreview

import (
	"context"
	"reflect"
	"strings"
	"testing"
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

	if slicesContainPrefix(env, "GH_TOKEN=") {
		t.Fatalf("expected GH_TOKEN to be removed: %v", env)
	}
	if slicesContainPrefix(env, "GITHUB_TOKEN=") {
		t.Fatalf("expected GITHUB_TOKEN to be removed: %v", env)
	}
	if slicesContainPrefix(env, "GH_ENTERPRISE_TOKEN=") {
		t.Fatalf("expected GH_ENTERPRISE_TOKEN to be removed: %v", env)
	}
	if slicesContainPrefix(env, "GITHUB_ENTERPRISE_TOKEN=") {
		t.Fatalf("expected GITHUB_ENTERPRISE_TOKEN to be removed: %v", env)
	}
	if !slicesContainExact(env, "GH_CONFIG_DIR=/tmp/config") {
		t.Fatalf("expected GH_CONFIG_DIR override: %v", env)
	}
	if !slicesContainExact(env, "GH_PROMPT_DISABLED=1") {
		t.Fatalf("expected GH_PROMPT_DISABLED=1: %v", env)
	}
	if !slicesContainExact(env, "KEEP=1") {
		t.Fatalf("expected unrelated env to survive: %v", env)
	}
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

	token, err := provider.Token(context.Background(), "/tmp/config")
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "token-value" {
		t.Fatalf("unexpected token %q", token)
	}
	if gotName != "gh" {
		t.Fatalf("unexpected command %q", gotName)
	}
	wantArgs := []string{"auth", "token", "--hostname", "github.com"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if !slicesContainExact(gotEnv, "GH_CONFIG_DIR=/tmp/config") {
		t.Fatalf("expected isolated GH_CONFIG_DIR: %v", gotEnv)
	}
	if slicesContainPrefix(gotEnv, "GH_TOKEN=") {
		t.Fatalf("expected GH_TOKEN to be scrubbed: %v", gotEnv)
	}
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
