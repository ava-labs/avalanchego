// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package skilltest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
)

const (
	AgentClaude = "claude"
	AgentCodex  = "codex"
)

// Config configures a skill test run.
type Config struct {
	// Agent selects the CLI used to run the test. Supported values are
	// "claude" and "codex". Defaults to "claude".
	Agent string
	// Path to the SKILL.md file (relative to test file or absolute).
	SkillPath string
	// Prompt to send to the agent.
	Prompt string
	// Timeout for the agent run.
	Timeout time.Duration
	// Model to use. If empty, the agent CLI default is used. This can be
	// overridden by SKILL_TEST_MODEL or SKILL_TEST_MODEL_<AGENT>.
	Model string
	// WorkDir sets the agent working directory. If empty, the current process
	// working directory is used.
	WorkDir string
	// Env adds or overrides environment variables for the spawned agent
	// process and any child commands it runs.
	Env map[string]string
	// BinWrappers maps command names to script content. Each entry creates
	// a wrapper script in a temp directory prepended to PATH, allowing tests
	// to intercept commands (e.g., redirect yt-dlp output to a temp dir).
	BinWrappers map[string]string
}

// Result holds the outcome of a skill test run.
type Result struct {
	// Path to file containing agent's stdout.
	// Output may be large; read as needed rather than loading entirely into memory.
	OutputPath string
	// Agent's exit code.
	ExitCode int
}

// Run spawns the configured agent with the skill context injected and returns the result
// for caller validation.
func Run(t *testing.T, cfg Config) Result {
	t.Helper()
	return run(t, cfg, true)
}

// RunWithout spawns the configured agent without skill context (for comparison).
// Agent failures are logged as informational, not test failures.
func RunWithout(t *testing.T, cfg Config) Result {
	t.Helper()
	return run(t, cfg, false)
}

func resolveAgent(cfg Config) string {
	if cfg.Agent == "" {
		return AgentClaude
	}
	return cfg.Agent
}

func resolveModel(cfg Config) string {
	agent := strings.ToUpper(resolveAgent(cfg))
	if env := os.Getenv("SKILL_TEST_MODEL_" + agent); env != "" {
		return env
	}
	if env := os.Getenv("SKILL_TEST_MODEL"); env != "" {
		return env
	}
	if cfg.Model != "" {
		return cfg.Model
	}
	return ""
}

// readSkillFile reads the SKILL.md content, resolving the path relative to the
// calling test file's directory.
func readSkillFile(skillPath string) (string, error) {
	if filepath.IsAbs(skillPath) {
		content, err := os.ReadFile(skillPath)
		if err != nil {
			return "", stacktrace.Errorf("reading skill file %s: %w", skillPath, err)
		}
		return string(content), nil
	}

	// Resolve relative to the caller's test file location.
	// Skip 3: readSkillFile(0), run(1), Run/RunWithout(2) -> caller at 3
	_, callerFile, _, ok := runtime.Caller(3)
	if !ok {
		return "", stacktrace.Errorf("unable to determine caller file for relative skill path %s", skillPath)
	}
	absPath := filepath.Join(filepath.Dir(callerFile), skillPath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", stacktrace.Errorf("reading skill file %s: %w", absPath, err)
	}
	return string(content), nil
}

func wrapPromptWithSkill(skillContent, prompt string) string {
	return "Follow these skill instructions exactly:\n\n" +
		skillContent +
		"\n\nUser request:\n" +
		prompt
}

func run(t *testing.T, cfg Config, withSkill bool) Result {
	t.Helper()

	logger := zaptest.NewLogger(t).Named(t.Name())

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}

	agent := resolveAgent(cfg)
	model := resolveModel(cfg)
	workDir := cfg.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		require.NoError(t, err, "failed to determine working directory")
	}

	prompt := cfg.Prompt
	var skillContent string

	if withSkill {
		var err error
		skillContent, err = readSkillFile(cfg.SkillPath)
		require.NoError(t, err, "failed to read skill file")
		logger.Info("loaded skill file",
			zap.String("path", cfg.SkillPath),
			zap.Int("contentLen", len(skillContent)),
		)
	}

	command := agent
	var args []string
	switch agent {
	case AgentClaude:
		args = []string{
			"--print",
			"--dangerously-skip-permissions",
			"--disable-slash-commands",
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		if withSkill {
			args = append(args, "--append-system-prompt", skillContent)
		}
		args = append(args, prompt)
	case AgentCodex:
		args = []string{
			"exec",
			"-C", workDir,
			"--dangerously-bypass-approvals-and-sandbox",
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		if withSkill {
			prompt = wrapPromptWithSkill(skillContent, prompt)
		}
		args = append(args, prompt)
	default:
		require.Failf(t, "unsupported agent", "unsupported agent %q", agent)
	}

	outFile, err := os.CreateTemp(t.TempDir(), "agent-output-*.txt")
	require.NoError(t, err, "failed to create output file")
	outPath := outFile.Name()

	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	cmd.Env = filteredEnv()
	cmd.Env = mergeEnv(cmd.Env, cfg.Env)

	if len(cfg.BinWrappers) > 0 {
		binDir := filepath.Join(t.TempDir(), "bin")
		require.NoError(t, os.MkdirAll(binDir, 0o755), "failed to create bin wrapper dir")
		for name, content := range cfg.BinWrappers {
			p := filepath.Join(binDir, name)
			require.NoError(t, os.WriteFile(p, []byte(content), 0o755), "failed to write bin wrapper %s", name)
			logger.Info("installed bin wrapper",
				zap.String("name", name),
				zap.String("path", p),
			)
		}
		// Prepend wrapper dir to PATH so wrappers shadow real binaries.
		for i, e := range cmd.Env {
			if strings.HasPrefix(e, "PATH=") {
				cmd.Env[i] = "PATH=" + binDir + ":" + e[5:]
				break
			}
		}
	}

	cmd.Stdout = outFile
	cmd.Stderr = outFile

	logger.Info("spawning agent",
		zap.String("agent", agent),
		zap.Bool("withSkill", withSkill),
		zap.String("model", model),
		zap.Duration("timeout", timeout),
		zap.String("workDir", workDir),
		zap.String("outputPath", outPath),
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var runErr error
	select {
	case runErr = <-errCh:
	case <-timer.C:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		require.Failf(t, "agent timed out", "%s timed out after %s", agent, timeout)
	}

	_ = outFile.Close()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			if withSkill {
				require.NoError(t, runErr, "failed to run %s", agent)
			}
			logger.Info("agent failed to run",
				zap.Bool("withSkill", withSkill),
				zap.Error(runErr),
			)
			exitCode = -1
		}
	}

	logger.Info("agent finished",
		zap.String("agent", agent),
		zap.Bool("withSkill", withSkill),
		zap.Int("exitCode", exitCode),
		zap.String("outputPath", outPath),
	)

	if !withSkill && exitCode != 0 {
		logger.Info("non-zero exit informational",
			zap.Bool("withSkill", withSkill),
			zap.Int("exitCode", exitCode),
		)
	}

	return Result{
		OutputPath: outPath,
		ExitCode:   exitCode,
	}
}

// filteredEnv returns the current environment with Claude Code session
// variables removed so that the spawned claude process does not detect
// a nested session and refuse to start.
func filteredEnv() []string {
	blocked := map[string]bool{
		"CLAUDECODE":             true,
		"CLAUDE_CODE_ENTRYPOINT": true,
	}
	var env []string
	for _, e := range os.Environ() {
		idx := strings.Index(e, "=")
		if idx < 0 {
			env = append(env, e)
			continue
		}
		key := e[:idx]
		if !blocked[key] {
			env = append(env, e)
		}
	}
	return env
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	merged := append([]string(nil), base...)
	indexByKey := make(map[string]int, len(merged))
	for i, entry := range merged {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		indexByKey[key] = i
	}

	for key, value := range overrides {
		entry := key + "=" + value
		if idx, ok := indexByKey[key]; ok {
			merged[idx] = entry
			continue
		}
		merged = append(merged, entry)
	}
	return merged
}
