// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	bazelDiffURL     = "https://github.com/Tinder/bazel-diff/releases/download/v22.0.0/bazel-diff_deploy.jar"
	bazelDiffHash    = "sha256-F7opo1MmvosIVObhv29FA2gq9bBBZfeaPn+96apq5uY="
	bazelDiffSubdir  = "impactedtests/bazel-diff"
	bazelDiffJarName = "bazel-diff_deploy.jar"
)

func impactedLabels(ctx context.Context, rangeArg string) ([]string, error) {
	repoRoot, err := gitOutput(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}

	diff, err := parseDiffRange(rangeArg)
	if err != nil {
		return nil, fmt.Errorf("parse range: %w", err)
	}

	if err := ensureRevisionAvailable(ctx, repoRoot, diff.baseRev); err != nil {
		return nil, err
	}
	if diff.headRev != "" {
		if err := ensureRevisionAvailable(ctx, repoRoot, diff.headRev); err != nil {
			return nil, err
		}
	}

	jarPath, err := prefetchBazelDiff(ctx)
	if err != nil {
		return nil, err
	}

	scratchDir, err := os.MkdirTemp("", "impactedtests-")
	if err != nil {
		return nil, fmt.Errorf("create scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchDir)

	bazelPath, err := exec.LookPath("bazel")
	if err != nil {
		return nil, fmt.Errorf("find bazel: %w", err)
	}

	basePath := filepath.Join(scratchDir, "base")
	if err := runGit(ctx, repoRoot, "worktree", "add", "--detach", basePath, diff.baseRev); err != nil {
		return nil, fmt.Errorf("create base worktree: %w", err)
	}
	defer func() {
		if err := runGit(ctx, repoRoot, "worktree", "remove", "--force", basePath); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: remove base worktree %s: %v\n", basePath, err)
		}
	}()

	headPath := repoRoot
	if !diff.includeWorkingTree {
		headPath = filepath.Join(scratchDir, "head")
		if err := runGit(ctx, repoRoot, "worktree", "add", "--detach", headPath, diff.headRev); err != nil {
			return nil, fmt.Errorf("create head worktree: %w", err)
		}
		defer func() {
			if err := runGit(ctx, repoRoot, "worktree", "remove", "--force", headPath); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: remove head worktree %s: %v\n", headPath, err)
			}
		}()
	}

	baseHashes := filepath.Join(scratchDir, "base-hashes.json")
	headHashes := filepath.Join(scratchDir, "head-hashes.json")
	impactedTargets := filepath.Join(scratchDir, "impacted-targets.txt")

	if err := runBazelDiff(ctx, jarPath, "generate-hashes", "-w", basePath, "-b", bazelPath, baseHashes); err != nil {
		return nil, fmt.Errorf("generate base hashes: %w", err)
	}
	if err := runBazelDiff(ctx, jarPath, "generate-hashes", "-w", headPath, "-b", bazelPath, headHashes); err != nil {
		return nil, fmt.Errorf("generate head hashes: %w", err)
	}
	if err := runBazelDiff(ctx, jarPath, "get-impacted-targets", "-w", headPath, "-b", bazelPath, "-sh", baseHashes, "-fh", headHashes, "-o", impactedTargets); err != nil {
		return nil, fmt.Errorf("compute impacted targets: %w", err)
	}

	return readLines(impactedTargets)
}

func impactedManifest(ctx context.Context, rangeArg string, scopes []string) ([]string, error) {
	return impactedGoTestManifest(ctx, rangeArg, scopes)
}

func queryDirectoryForRange(ctx context.Context, rangeArg string) (string, func(), error) {
	diff, err := parseDiffRange(rangeArg)
	if err != nil {
		return "", nil, fmt.Errorf("parse range: %w", err)
	}
	repoRoot, err := gitOutput(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, fmt.Errorf("resolve repo root: %w", err)
	}

	queryDir := repoRoot
	cleanup := func() {}
	if !diff.includeWorkingTree {
		scratchDir, err := os.MkdirTemp("", "impactedtests-query-")
		if err != nil {
			return "", nil, fmt.Errorf("create query scratch dir: %w", err)
		}

		queryDir = filepath.Join(scratchDir, "head")
		if err := runGit(ctx, repoRoot, "worktree", "add", "--detach", queryDir, diff.headRev); err != nil {
			os.RemoveAll(scratchDir)
			return "", nil, fmt.Errorf("create query worktree: %w", err)
		}
		cleanup = func() {
			_ = runGit(ctx, repoRoot, "worktree", "remove", "--force", queryDir)
			_ = os.RemoveAll(scratchDir)
		}
	}
	return queryDir, cleanup, nil
}

func queryScopedGoTests(ctx context.Context, dir string, scopes []string) ([]string, error) {
	scopeExpr, err := scopeExpression(scopes)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`kind("go_test rule", %s) except attr("tags", "manual", kind("go_test rule", %s))`, scopeExpr, scopeExpr)
	output, err := commandOutput(ctx, dir, "bazel", "query", query)
	if err != nil {
		return nil, fmt.Errorf("query non-manual go_test targets for %q: %w", scopeExpr, err)
	}
	return splitLines(output), nil
}

func prefetchBazelDiff(ctx context.Context) (string, error) {
	if jarPath := os.Getenv("BAZEL_DIFF_JAR"); jarPath != "" {
		if err := validateBazelDiffFile(jarPath); err != nil {
			return "", fmt.Errorf("validate bazel-diff jar %q: %w", jarPath, err)
		}
		return jarPath, nil
	}

	cacheDir, err := bazelDiffCacheDir()
	if err != nil {
		return "", err
	}
	jarPath := filepath.Join(cacheDir, bazelDiffJarName)
	if err := validateBazelDiffFile(jarPath); err == nil {
		return jarPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(jarPath)
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create bazel-diff cache dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(cacheDir, bazelDiffJarName+".*")
	if err != nil {
		return "", fmt.Errorf("create bazel-diff temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := downloadFile(ctx, bazelDiffURL, tmpFile); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("download bazel-diff: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close bazel-diff temp file: %w", err)
	}
	if err := validateBazelDiffFile(tmpPath); err != nil {
		return "", fmt.Errorf("validate downloaded bazel-diff: %w", err)
	}
	if err := os.Rename(tmpPath, jarPath); err != nil {
		return "", fmt.Errorf("install bazel-diff jar: %w", err)
	}
	return jarPath, nil
}

func bazelDiffCacheDir() (string, error) {
	if cacheDir := os.Getenv("BAZEL_DIFF_CACHE_DIR"); cacheDir != "" {
		return cacheDir, nil
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(userCacheDir, bazelDiffSubdir), nil
}

func downloadFile(ctx context.Context, url string, file *os.File) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", response.Status)
	}
	_, err = io.Copy(file, response.Body)
	return err
}

func validateBazelDiffFile(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	wantHash, err := decodeSRIHash(bazelDiffHash)
	if err != nil {
		return err
	}
	gotHash := sha256.Sum256(contents)
	if !strings.EqualFold(base64.StdEncoding.EncodeToString(gotHash[:]), base64.StdEncoding.EncodeToString(wantHash)) {
		return fmt.Errorf("unexpected bazel-diff hash %q", "sha256-"+base64.StdEncoding.EncodeToString(gotHash[:]))
	}
	return nil
}

func decodeSRIHash(hash string) ([]byte, error) {
	algorithm, encoded, ok := strings.Cut(hash, "-")
	if !ok {
		return nil, fmt.Errorf("invalid SRI hash %q", hash)
	}
	if algorithm != "sha256" {
		return nil, fmt.Errorf("unsupported SRI hash algorithm %q", algorithm)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode SRI hash %q: %w", hash, err)
	}
	return decoded, nil
}

func runBazelDiff(ctx context.Context, jarPath string, args ...string) error {
	javaPath, err := resolveJavaPath(ctx)
	if err != nil {
		return fmt.Errorf("resolve java: %w", err)
	}
	_, err = commandOutput(ctx, "", javaPath, append([]string{"-jar", jarPath}, args...)...)
	return err
}

func resolveJavaPath(ctx context.Context) (string, error) {
	if javaPath, err := exec.LookPath("java"); err == nil {
		return javaPath, nil
	}

	javaHome, err := commandOutput(ctx, "", "bazel", "info", "java-home")
	if err != nil {
		return "", fmt.Errorf("query bazel java-home: %w", err)
	}
	javaPath := filepath.Join(javaHome, "bin", "java")
	if _, err := os.Stat(javaPath); err != nil {
		return "", fmt.Errorf("stat bazel java %q: %w", javaPath, err)
	}
	return javaPath, nil
}

func ensureRevisionAvailable(ctx context.Context, repoRoot string, rev string) error {
	if err := runGit(ctx, repoRoot, "rev-parse", "--verify", rev+"^{commit}"); err == nil {
		return nil
	}
	if !isLikelySHA(rev) {
		return fmt.Errorf("revision %q is not available locally", rev)
	}
	if err := runGit(ctx, repoRoot, "fetch", "--no-tags", "--depth=1", "origin", rev); err != nil {
		return fmt.Errorf("fetch revision %q: %w", rev, err)
	}
	if err := runGit(ctx, repoRoot, "rev-parse", "--verify", rev+"^{commit}"); err != nil {
		return fmt.Errorf("verify fetched revision %q: %w", rev, err)
	}
	return nil
}

func isLikelySHA(rev string) bool {
	if len(rev) < 7 || len(rev) > 40 {
		return false
	}
	for _, r := range rev {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	return commandOutput(ctx, "", "git", args...)
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := commandOutput(ctx, dir, "git", args...)
	return err
}

func commandOutput(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		trimmedStdout := strings.TrimSpace(string(output))
		trimmedStderr := strings.TrimSpace(stderr.String())
		trimmed := strings.TrimSpace(strings.Join([]string{trimmedStdout, trimmedStderr}, ": "))
		if trimmed == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, trimmed)
	}
	return strings.TrimSpace(string(output)), nil
}

func readLines(path string) ([]string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return splitLines(string(bytes)), nil
}

func splitLines(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}
