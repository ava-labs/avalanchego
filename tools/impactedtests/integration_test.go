// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestNoChanges(t *testing.T) {
	sourceRoot := sourceRepoRootForTest(t)
	toolPath := buildToolForTest(t, sourceRoot)
	repoRoot := materializeRepoForTest(t, sourceRoot)

	output := runToolForTest(t, repoRoot, toolPath, "manifest", "--range", "HEAD..HEAD", "--scope", "//...", "--scope", "-//graft/...")
	require.Empty(t, output)
}

func TestManifestChangedTestFileOnly(t *testing.T) {
	sourceRoot := sourceRepoRootForTest(t)
	toolPath := buildToolForTest(t, sourceRoot)
	repoRoot := materializeRepoForTest(t, sourceRoot)

	appendLineForTest(t, filepath.Join(repoRoot, "utils/set/set_test.go"), "// impactedtests test-only integration change")

	output := runToolForTest(t, repoRoot, toolPath, "manifest", "--range", "HEAD..", "--scope", "//...", "--scope", "-//graft/...")
	require.Equal(t, []string{"//utils/set:set_test"}, splitLines(output))
}

func TestManifestChangedSharedUtilityLibrary(t *testing.T) {
	sourceRoot := sourceRepoRootForTest(t)
	toolPath := buildToolForTest(t, sourceRoot)
	repoRoot := materializeRepoForTest(t, sourceRoot)

	appendLineForTest(t, filepath.Join(repoRoot, "utils/atomic.go"), "// impactedtests shared-library integration change")

	output := runToolForTest(t, repoRoot, toolPath, "manifest", "--range", "HEAD..", "--scope", "//...", "--scope", "-//graft/...")
	labels := splitLines(output)
	require.Contains(t, labels, "//utils:utils_test")
	require.Contains(t, labels, "//api/admin:admin_test")
	require.Greater(t, len(labels), 1)
}

func sourceRepoRootForTest(t *testing.T) string {
	t.Helper()

	if sourceRoot := os.Getenv("IMPACTEDTESTS_SOURCE_REPO"); sourceRoot != "" {
		return sourceRoot
	}
	if os.Getenv("TEST_SRCDIR") != "" && os.Getenv("TEST_WORKSPACE") != "" {
		t.Fatal("IMPACTEDTESTS_SOURCE_REPO must be set for Bazel integration tests")
	}
	return strings.TrimSpace(runGitForTest(t, "", "rev-parse", "--show-toplevel"))
}

func materializeRepoForTest(t *testing.T, sourceRoot string) string {
	t.Helper()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, copyTreeForTest(sourceRoot, repoRoot))

	runGitForTest(t, repoRoot, "init")
	runGitForTest(t, repoRoot, "config", "user.email", "impactedtests@example.invalid")
	runGitForTest(t, repoRoot, "config", "user.name", "impactedtests integration test")
	runGitForTest(t, repoRoot, "add", ".")
	runGitForTest(t, repoRoot, "commit", "-m", "baseline")
	return repoRoot
}

func buildToolForTest(t *testing.T, repoRoot string) string {
	t.Helper()

	if binaryPath := bazelBinaryPathForTest(); binaryPath != "" {
		return binaryPath
	}

	binaryDir := t.TempDir()
	binaryPath := filepath.Join(binaryDir, "impactedtests")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./tools/impactedtests")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	require.NoError(t, execError("build impactedtests binary", err, output))
	return binaryPath
}

func appendLineForTest(t *testing.T, path string, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	defer file.Close()
	_, err = file.WriteString("\n" + line + "\n")
	require.NoError(t, err)
}

func runToolForTest(t *testing.T, dir string, toolPath string, args ...string) string {
	t.Helper()

	pathEnv := selectorPathEnvForTest(t)
	cacheDir := filepath.Join(t.TempDir(), "bazel-diff-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))

	cmd := exec.Command(toolPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+pathEnv,
		"BAZEL_DIFF_CACHE_DIR="+cacheDir,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, execError(toolPath, err, output))
	return strings.TrimSpace(string(output))
}

func runGitForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, execError("git", err, output))
	return string(output)
}

func bazelBinaryPathForTest() string {
	binaryPath := os.Getenv("IMPACTEDTESTS_TEST_BINARY")
	if binaryPath == "" {
		return ""
	}
	if filepath.IsAbs(binaryPath) {
		return binaryPath
	}

	testSrcDir := os.Getenv("TEST_SRCDIR")
	testWorkspace := os.Getenv("TEST_WORKSPACE")
	if testSrcDir == "" || testWorkspace == "" {
		return binaryPath
	}
	return filepath.Join(testSrcDir, testWorkspace, binaryPath)
}

func selectorPathEnvForTest(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("bazel"); err == nil {
		return os.Getenv("PATH")
	}

	bazeliskPath, err := exec.LookPath("bazelisk")
	require.NoError(t, err)

	binDir := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.Symlink(bazeliskPath, filepath.Join(binDir, "bazel")))
	return binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func copyTreeForTest(sourceRoot string, destRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return os.MkdirAll(destRoot, 0o755)
		}
		if shouldSkipPathForTest(relPath) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(destRoot, relPath)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			targetInfo, err := os.Stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if targetInfo.IsDir() {
				return nil
			}
			return copyFileForTest(path, destPath, targetInfo.Mode().Perm())
		}
		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode().Perm())
		}
		return copyFileForTest(path, destPath, info.Mode().Perm())
	})
}

func shouldSkipPathForTest(relPath string) bool {
	parts := strings.Split(relPath, string(filepath.Separator))
	for _, part := range parts {
		if part == ".git" || part == ".direnv" {
			return true
		}
		if strings.HasPrefix(part, "bazel-") {
			return true
		}
	}
	return false
}

func copyFileForTest(sourcePath string, destPath string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func execError(action string, err error, output []byte) error {
	if err == nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return err
	}
	return fmt.Errorf("%s: %w: %s", action, err, trimmed)
}
