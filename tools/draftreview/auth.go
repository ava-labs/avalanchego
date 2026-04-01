package draftreview

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type TokenProvider interface {
	Token(ctx context.Context, configDir string) (string, error)
}

type ExecFunc func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)

type GHTokenProvider struct {
	exec ExecFunc
	env  []string
}

func NewGHTokenProvider() GHTokenProvider {
	return GHTokenProvider{
		exec: runCommand,
		env:  os.Environ(),
	}
}

func (p GHTokenProvider) Token(ctx context.Context, configDir string) (string, error) {
	output, err := p.exec(ctx, isolatedGitHubEnv(p.env, configDir), "gh", "auth", "token", "--hostname", "github.com")
	if err != nil {
		return "", fmt.Errorf("acquire GitHub token with isolated gh auth: %w", err)
	}

	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned an empty token")
	}
	return token, nil
}

func isolatedGitHubEnv(baseEnv []string, configDir string) []string {
	filtered := make([]string, 0, len(baseEnv)+2)
	for _, entry := range baseEnv {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		switch key {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN":
			continue
		case "GH_CONFIG_DIR":
			continue
		}
		filtered = append(filtered, entry)
	}

	filtered = append(filtered, "GH_CONFIG_DIR="+configDir)
	filtered = append(filtered, "GH_PROMPT_DISABLED=1")
	return filtered
}

func runCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() == 0 {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
