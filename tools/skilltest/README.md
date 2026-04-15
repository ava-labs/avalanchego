# skilltest

`skilltest` is a Go test helper for exercising an agent skill end to end from a
Go test.

It starts either `claude` or `codex`, injects a skill's `SKILL.md`, sends a
test prompt, and returns the exit code plus a path to the captured combined
stdout/stderr output.

This package is most useful for tests that need to verify a skill's real
behavior rather than just inspect its text.

## What It Does

- Resolves `SKILL.md` from an absolute path or relative to the calling test file
- Spawns either `claude` or `codex`
- Injects the skill content agent-appropriately:
  `claude` via `--append-system-prompt`, `codex` by wrapping the prompt
- Removes nested-session environment variables before launching Claude
- Captures combined stdout/stderr to a temp file
- Exposes `SKILLTEST_SKILL_DIR` to the spawned agent process when a skill is
  injected, pointing at the resolved skill directory for harness-only fallbacks
  such as skill-bundled script launchers
- Supports per-test command wrappers by prepending a temporary directory to
  `PATH`

## API

The main entrypoints are:

- `Run(t, cfg)` for a normal skill-backed run
- `RunWithout(t, cfg)` for the same prompt without injecting the skill, useful
  for comparison tests

`Config` fields:

- `Agent`: agent CLI to run; supported values are `claude` and `codex`;
  defaults to `claude`
- `SkillPath`: path to `SKILL.md`; relative paths are resolved from the calling
  test file
- `Prompt`: prompt sent to the agent
- `Timeout`: run timeout; defaults to `2 * time.Minute`
- `Model`: model override; if empty, the agent CLI default is used
- `WorkDir`: working directory for the spawned agent
- `BinWrappers`: map of command name to shell script content; wrappers are
  installed into a temp directory placed first on `PATH`

Model selection order:

1. `SKILL_TEST_MODEL_<AGENT>` environment variable
1. `SKILL_TEST_MODEL` environment variable
2. `Config.Model`
3. agent CLI default

`Result` contains:

- `OutputPath`: temp file containing combined stdout/stderr
- `ExitCode`: Claude process exit code

## Typical Usage

The pattern in dotfiles is:

1. Put a test beside the skill, usually at `<skill>/tests/skill_test.go`
2. Call `skilltest.Run(...)` with `SkillPath: "../SKILL.md"`
3. Assert on observable side effects such as files written, command invocations,
   or process output
4. Read `result.OutputPath` when you need the full transcript for debugging or
   additional assertions

Example:

```go
package tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tools/skilltest"
)

func TestSkill(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))

	result := skilltest.Run(t, skilltest.Config{
		Agent:     skilltest.AgentClaude,
		SkillPath: "../SKILL.md",
		WorkDir:   "/path/to/repo/root",
		Prompt:    "Perform the task under test",
		Timeout:   2 * time.Minute,
	})

	require.Equal(t, 0, result.ExitCode)
}
```

## Wrapping External Commands

`BinWrappers` lets a test intercept commands the skill is expected to run. The
dotfiles `youtube-download` skill uses this to shadow `yt-dlp` with a wrapper
that forces downloads into a temp directory while still delegating to the real
binary.

That pattern is useful when the skill:

- invokes external tools
- writes to uncontrolled paths by default
- needs lightweight command-level instrumentation without changing the skill

## Notes

- `RunWithout` does not fail the test if Claude itself fails to start; it is
  intended for comparison and baseline checks.
- The package shells out to the selected agent CLI, so tests require a working
  `claude` or `codex` installation.
- `OutputPath` is returned instead of the full output string so large transcripts
  do not get loaded into memory by default.
