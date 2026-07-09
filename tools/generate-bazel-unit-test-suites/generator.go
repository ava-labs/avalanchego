// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// shardFile mirrors the checked-in JSON document shape. The file stores the
// shard list under a top-level "shards" key rather than as a bare JSON array,
// so unmarshaling directly into []shard would not match the on-disk format.
type shardFile struct {
	Shards []shard `json:"shards"`
}

type shard struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Query       string `json:"query"`
}

type queryRunner func(query string) ([]string, error)

var (
	errDuplicateShardName    = errors.New("unit-test shard name is duplicated")
	errEmptyShard            = errors.New("unit-test shard has no runnable Go test targets")
	errEmptyShardDescription = errors.New("unit-test shard description is empty")
	errEmptyShardName        = errors.New("unit-test shard name is empty")
	errEmptyShardQuery       = errors.New("unit-test shard query is empty")
	errNonGoTests            = errors.New("unit-test shard selects non-Go test targets")
	errOverlappingTests      = errors.New("unit-test shards select the same Go test targets")
	errUnassignedTests       = errors.New("unit-test shards do not select all runnable Go test targets")
)

func parseShardFile(contents []byte) ([]shard, error) {
	var metadata shardFile
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return nil, err
	}
	return metadata.Shards, nil
}

func validateShardDefinitions(shards []shard) error {
	names := make(map[string]struct{}, len(shards))
	for _, shard := range shards {
		if shard.Name == "" {
			return errEmptyShardName
		}
		if shard.Query == "" {
			return fmt.Errorf("%w: %q", errEmptyShardQuery, shard.Name)
		}
		if strings.TrimSpace(shard.Description) == "" {
			return fmt.Errorf("%w: %q", errEmptyShardDescription, shard.Name)
		}
		if _, exists := names[shard.Name]; exists {
			return fmt.Errorf("%w: %q", errDuplicateShardName, shard.Name)
		}
		names[shard.Name] = struct{}{}
	}
	return nil
}

func generateSuiteFile(shards []shard, runQuery queryRunner) (string, error) {
	if err := validateShardDefinitions(shards); err != nil {
		return "", err
	}

	suites := make(map[string][]string, len(shards))
	for _, shard := range shards {
		nonGoTests, err := runQuery(nonGoTestsQuery(shard.Query))
		if err != nil {
			return "", err
		}
		if len(nonGoTests) > 0 {
			return "", fmt.Errorf(
				"%w in shard %q:\n  %s\nall Bazel unit-test shards in .bazel/test_shards.json must stay Go-only; fix the shard query rather than making non-Go tests tolerate Go test flags",
				errNonGoTests,
				shard.Name,
				strings.Join(nonGoTests, "\n  "),
			)
		}

		targets, err := runQuery(runnableTestsQuery(shard.Query))
		if err != nil {
			return "", err
		}
		if len(targets) == 0 {
			return "", fmt.Errorf("%w: %q", errEmptyShard, shard.Name)
		}
		suites[shard.Name] = targets
	}

	allTests, err := runQuery(allRunnableGoTestsQuery())
	if err != nil {
		return "", err
	}
	if err := validateSuiteMembership(shards, suites, allTests); err != nil {
		return "", err
	}

	return renderSuiteFile(shards, suites), nil
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
	var overlaps []string
	for _, target := range allTests {
		switch targetOwners := owners[target]; len(targetOwners) {
		case 0:
			unassigned = append(unassigned, target)
		case 1:
		default:
			overlaps = append(overlaps, fmt.Sprintf("%s (%s)", target, strings.Join(targetOwners, ", ")))
		}
	}
	if len(unassigned) > 0 {
		return fmt.Errorf("%w:\n  %s", errUnassignedTests, strings.Join(unassigned, "\n  "))
	}
	if len(overlaps) > 0 {
		return fmt.Errorf("%w:\n  %s", errOverlappingTests, strings.Join(overlaps, "\n  "))
	}
	return nil
}

func nonGoTestsQuery(query string) string {
	// These test groups must contain Go tests only because CI passes Go test
	// options such as `-test.shuffle` to them. Exclude manual tests first
	// because generated suites do not name them.
	runnableTests := runnableTestsQuery(query)
	return fmt.Sprintf("(%s) except kind(\"go_test rule\", (%s))", runnableTests, runnableTests)
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
		for _, line := range strings.Split(shard.Description, "\n") {
			b.WriteString(fmt.Sprintf("    # %s\n", line))
		}
		b.WriteString(fmt.Sprintf("    %q: [\n", shard.Name))
		for _, target := range suites[shard.Name] {
			b.WriteString(fmt.Sprintf("        %q,\n", target))
		}
		b.WriteString("    ],\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func sortedNonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	slices.Sort(lines)
	return lines
}
