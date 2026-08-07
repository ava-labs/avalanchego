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
			return errors.New("found shard with empty name")
		}
		if shard.Query == "" {
			return fmt.Errorf("shard %q has empty query", shard.Name)
		}
		if _, exists := names[shard.Name]; exists {
			return fmt.Errorf("found duplicate shard name %q", shard.Name)
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
				"unit-test shard %q selects non-Go test targets:\n  %s\nall Bazel unit-test shards in .bazel/test_shards.json must stay Go-only; fix the shard query rather than making non-Go tests tolerate Go test flags",
				shard.Name,
				strings.Join(nonGoTests, "\n  "),
			)
		}

		targets, err := runQuery(runnableTestsQuery(shard.Query))
		if err != nil {
			return "", err
		}
		suites[shard.Name] = targets
	}

	return renderSuiteFile(shards, suites), nil
}

func runnableTestsQuery(query string) string {
	// `bazel test //...` skips tests tagged `manual` unless you name them
	// directly. The generated suites name tests directly, so filter `manual`
	// tests out here to keep the usual unit-test behavior.
	return fmt.Sprintf("(%s) except attr(\"tags\", \"manual\", (%s))", query, query)
}

func nonGoTestsQuery(query string) string {
	// These test groups must contain Go tests only because CI passes Go test
	// options such as `-test.shuffle` to them. Reject non-Go tests here so
	// mistakes are caught when the file is generated.
	return fmt.Sprintf("(%s) except kind(\"go_test rule\", (%s))", query, query)
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
