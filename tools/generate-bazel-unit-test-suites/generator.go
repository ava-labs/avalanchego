// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/ava-labs/avalanchego/utils/perms"
)

// shardFile mirrors the form of the JSON document that is checked-in. The file stores
// the shard list under a top-level "shards" key rather than as a bare JSON array, so
// unmarshaling directly into []shard would not match the on-disk format.
type shardFile struct {
	Shards []shard `json:"shards"`
}

type shard struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Query       string `json:"query"`
}

// queryTarget is one line of Bazel's `--output=label_kind` output. The kind
// includes the trailing "rule", for example "go_test rule". Keeping the kind
// lets one query both collect suite members and reject non-Go tests.
type queryTarget struct {
	kind  string
	label string
}

// queryRunner abstracts Bazel queries so membership validation can be tested
// without starting Bazel.
type queryRunner func(query string) ([]queryTarget, error)

type (
	readFile  func(string) ([]byte, error)
	writeFile func(string, []byte, os.FileMode) error
)

var (
	errDuplicateShardName    = errors.New("unit-test shard name is duplicated")
	errEmptyShard            = errors.New("unit-test shard has no non-manual Go test targets")
	errEmptyShardDescription = errors.New("unit-test shard description is empty")
	errEmptyShardName        = errors.New("unit-test shard name is empty")
	errEmptyShardQuery       = errors.New("unit-test shard query is empty")
	errNonGoTests            = errors.New("unit-test shard selects non-Go test targets")
	errOverlappingTests      = errors.New("unit-test shards select the same Go test targets")
	errUnassignedTests       = errors.New("unit-test shards do not select all non-manual Go test targets")
)

// run generates the checked-in unit-test suites from the shard definitions.
func run(ctx context.Context) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	return generate(
		filepath.Join(root, ".bazel", "test_shards.json"),
		filepath.Join(root, ".bazel", "generated_test_suites.bzl"),
		os.ReadFile,
		perms.WriteFile,
		func(query string) ([]queryTarget, error) {
			return bazelQuery(ctx, root, query)
		},
	)
}

// repoRoot returns the repository root for both Bazel and direct execution.
func repoRoot() (string, error) {
	// `bazel run` sets BUILD_WORKSPACE_DIRECTORY to the repository root. Use it
	// when available so this tool updates the checked-in file in the repository,
	// not in Bazel's temporary run location.
	if root := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); root != "" {
		return root, nil
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("failed to locate source file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file))), nil
}

// generate reads the shard definitions, generates the suite file, and writes it.
// Its side effects are parameters so tests can cover read, query, and write
// failures without modifying the repository or starting Bazel.
func generate(shardsPath string, outputPath string, readFile readFile, writeFile writeFile, runQuery queryRunner) error {
	contents, err := readFile(shardsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", shardsPath, err)
	}

	shards, err := parseShardFile(contents)
	if err != nil {
		return fmt.Errorf("parse %s: %w", shardsPath, err)
	}

	output, err := generateSuiteFile(shards, runQuery)
	if err != nil {
		return fmt.Errorf("generate %s: %w", outputPath, err)
	}

	if err := writeFile(outputPath, []byte(output), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}

// bazelQuery runs query in root and returns its sorted target kinds and labels.
// `--output=label_kind` preserves each rule's kind, which lets the generator
// reject non-Go tests without running a separate query for each shard.
func bazelQuery(ctx context.Context, root string, query string) ([]queryTarget, error) {
	cmd := exec.CommandContext(ctx, "bazelisk", "query", "--output=label_kind", query)
	cmd.Dir = root

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			_, _ = os.Stderr.Write(stderr.Bytes())
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("bazelisk query failed with exit code %d", exitErr.ExitCode())
		}
		return nil, fmt.Errorf("failed to run bazelisk query: %w", err)
	}

	return parseQueryTargets(stdout.String())
}

// parseQueryTargets converts Bazel's `<rule kind> <label>` output into query
// targets. Keep this format-specific parsing here so the membership logic only
// needs to decide which kinds it accepts.
func parseQueryTargets(output string) ([]queryTarget, error) {
	lines := sortedNonEmptyLines(output)
	targets := make([]queryTarget, len(lines))
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("unexpected bazel query output %q", line)
		}
		targets[i] = queryTarget{
			kind:  strings.Join(fields[:len(fields)-1], " "),
			label: fields[len(fields)-1],
		}
	}
	return targets, nil
}

func parseShardFile(contents []byte) ([]shard, error) {
	var metadata shardFile
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return nil, err
	}
	return metadata.Shards, nil
}

func generateSuiteFile(shards []shard, runQuery queryRunner) (string, error) {
	if err := validateShardDefinitions(shards); err != nil {
		return "", err
	}

	suites := make(map[string][]string, len(shards))
	for _, shard := range shards {
		// Query once per shard. The target kinds let this pass both select Go
		// tests and reject non-Go tests without a second Bazel invocation.
		targets, err := runQuery(runnableTestsQuery(shard.Query))
		if err != nil {
			return "", fmt.Errorf("query non-manual tests for shard %q: %w", shard.Name, err)
		}

		var goTests []string
		var nonGoTests []string
		for _, target := range targets {
			if target.kind == "go_test rule" {
				goTests = append(goTests, target.label)
			} else {
				nonGoTests = append(nonGoTests, target.label)
			}
		}
		if len(nonGoTests) > 0 {
			return "", fmt.Errorf(
				"%w in shard %q:\n  %s\nAll Bazel unit-test shards in .bazel/test_shards.json must contain Go tests only. Fix the shard query. Do not make non-Go tests accept Go test flags",
				errNonGoTests,
				shard.Name,
				strings.Join(nonGoTests, "\n  "),
			)
		}
		if len(goTests) == 0 {
			return "", fmt.Errorf("%w: %q", errEmptyShard, shard.Name)
		}
		suites[shard.Name] = goTests
	}

	allTargets, err := runQuery(allRunnableGoTestsQuery())
	if err != nil {
		return "", fmt.Errorf("query all non-manual Go tests: %w", err)
	}
	allTests := make([]string, len(allTargets))
	for i, target := range allTargets {
		allTests[i] = target.label
	}
	if err := validateSuiteMembership(shards, suites, allTests); err != nil {
		return "", err
	}

	return renderSuiteFile(shards, suites), nil
}

func validateShardDefinitions(shards []shard) error {
	names := make(map[string]struct{}, len(shards))
	var errs []error
	for _, shard := range shards {
		if shard.Name == "" {
			errs = append(errs, errEmptyShardName)
		}
		if shard.Query == "" {
			errs = append(errs, fmt.Errorf("%w: %q", errEmptyShardQuery, shard.Name))
		}
		if strings.TrimSpace(shard.Description) == "" {
			errs = append(errs, fmt.Errorf("%w: %q", errEmptyShardDescription, shard.Name))
		}
		if _, exists := names[shard.Name]; exists {
			errs = append(errs, fmt.Errorf("%w: %q", errDuplicateShardName, shard.Name))
		}
		names[shard.Name] = struct{}{}
	}
	return errors.Join(errs...)
}

func runnableTestsQuery(query string) string {
	// `bazel test //...` skips tests tagged `manual` unless you name them
	// directly. The generated suites name tests directly, so filter `manual`
	// tests out here to keep the usual unit-test behavior.
	return fmt.Sprintf("(%s) except attr(\"tags\", \"manual\", (%s))", query, query)
}

func allRunnableGoTestsQuery() string {
	return fmt.Sprintf("kind(\"go_test rule\", (%s))", runnableTestsQuery("tests(//...)"))
}

func validateSuiteMembership(shards []shard, suites map[string][]string, allTests []string) error {
	owners := make(map[string][]string, len(allTests))
	for _, shard := range shards {
		for _, target := range suites[shard.Name] {
			owners[target] = append(owners[target], shard.Name)
		}
	}

	var unassigned []string
	for _, target := range allTests {
		if len(owners[target]) == 0 {
			unassigned = append(unassigned, target)
		}
	}

	ownedTargets := make([]string, 0, len(owners))
	for target := range owners {
		ownedTargets = append(ownedTargets, target)
	}
	slices.Sort(ownedTargets)

	var overlaps []string
	for _, target := range ownedTargets {
		targetOwners := owners[target]
		if len(targetOwners) > 1 {
			overlaps = append(overlaps, fmt.Sprintf("%s (%s)", target, strings.Join(targetOwners, ", ")))
		}
	}

	var errs []error
	if len(unassigned) > 0 {
		errs = append(errs, fmt.Errorf("%w:\n  %s", errUnassignedTests, strings.Join(unassigned, "\n  ")))
	}
	if len(overlaps) > 0 {
		errs = append(errs, fmt.Errorf("%w:\n  %s", errOverlappingTests, strings.Join(overlaps, "\n  ")))
	}
	return errors.Join(errs...)
}

func renderSuiteFile(shards []shard, suites map[string][]string) string {
	// Iterate over the shard definitions rather than the suites map so the
	// generated file keeps the checked-in shard order from test_shards.json.
	var b strings.Builder
	b.WriteString("\"\"\"Generated unit-test shard suites. Do not edit by hand.\n\n")
	b.WriteString("Regenerate with: task bazel-generate-unit-test-suites\n")
	b.WriteString("\"\"\"\n\n")
	b.WriteString("UNIT_TEST_SHARDS = {\n")
	for _, shard := range shards {
		for line := range strings.SplitSeq(shard.Description, "\n") {
			fmt.Fprintf(&b, "    # %s\n", line)
		}
		fmt.Fprintf(&b, "    %q: [\n", shard.Name)
		for _, target := range suites[shard.Name] {
			fmt.Fprintf(&b, "        %q,\n", target)
		}
		b.WriteString("    ],\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func sortedNonEmptyLines(s string) []string {
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	slices.Sort(lines)
	return lines
}
