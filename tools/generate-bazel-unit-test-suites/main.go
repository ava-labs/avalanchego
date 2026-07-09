// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func repoRoot() string {
	// `bazel run` sets BUILD_WORKSPACE_DIRECTORY to the repository root. Use it
	// when available so this tool updates the checked-in file in the repository,
	// not in Bazel's temporary run location.
	if root := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); root != "" {
		return root
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fatalf("failed to locate source file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func bazelQuery(root string, query string) ([]string, error) {
	cmd := exec.Command("bazelisk", "query", query)
	cmd.Dir = root

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			_, _ = os.Stderr.Write(stderr.Bytes())
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("bazelisk query failed with exit code %d", exitErr.ExitCode())
		}
		return nil, fmt.Errorf("failed to run bazelisk query: %w", err)
	}

	return sortedNonEmptyLines(stdout.String()), nil
}

func main() {
	root := repoRoot()
	shardsPath := filepath.Join(root, ".bazel", "test_shards.json")
	outputPath := filepath.Join(root, ".bazel", "generated_test_suites.bzl")

	contents, err := os.ReadFile(shardsPath)
	if err != nil {
		fatalf("failed to read %s: %v", shardsPath, err)
	}

	shards, err := parseShardFile(contents)
	if err != nil {
		fatalf("failed to parse %s: %v", shardsPath, err)
	}

	output, err := generateSuiteFile(shards, func(query string) ([]string, error) {
		return bazelQuery(root, query)
	})
	if err != nil {
		fatalf("failed to generate %s: %v", outputPath, err)
	}

	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		fatalf("failed to write %s: %v", outputPath, err)
	}
}
