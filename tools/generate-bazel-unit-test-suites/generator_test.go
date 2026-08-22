// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func goTestTargets(labels ...string) []queryTarget {
	targets := make([]queryTarget, len(labels))
	for i, label := range labels {
		targets[i] = queryTarget{kind: "go_test rule", label: label}
	}
	return targets
}

func TestParseShardFile(t *testing.T) {
	contents := []byte(`{"shards":[{"name":"avalanchego_unit_tests","description":"AvalancheGo shard","query":"tests(//...)"}]}`)

	shards, err := parseShardFile(contents)
	require.NoError(t, err)

	expected := []shard{{
		Name:        "avalanchego_unit_tests",
		Description: "AvalancheGo shard",
		Query:       "tests(//...)",
	}}
	require.Equal(t, expected, shards)
}

func TestValidateShardDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		shards  []shard
		wantErr error
	}{
		{
			name: "valid",
			shards: []shard{{
				Name:        "avalanchego_unit_tests",
				Description: "AvalancheGo unit tests.",
				Query:       "tests(//...)",
			}},
		},
		{
			name: "empty description",
			shards: []shard{{
				Name:        "avalanchego_unit_tests",
				Description: " \n ",
				Query:       "tests(//...)",
			}},
			wantErr: errEmptyShardDescription,
		},
		{
			name: "empty name",
			shards: []shard{{
				Query: "tests(//...)",
			}},
			wantErr: errEmptyShardName,
		},
		{
			name: "empty query",
			shards: []shard{{
				Name: "avalanchego_unit_tests",
			}},
			wantErr: errEmptyShardQuery,
		},
		{
			name: "duplicate name",
			shards: []shard{
				{Name: "avalanchego_unit_tests", Description: "AvalancheGo unit tests.", Query: "tests(//... )"},
				{Name: "avalanchego_unit_tests", Description: "AvalancheGo unit tests.", Query: "tests(//foo/...)"},
			},
			wantErr: errDuplicateShardName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShardDefinitions(tt.shards)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestValidateShardDefinitionsReportsAllErrors(t *testing.T) {
	shards := []shard{
		{},
		{Name: "duplicate"},
		{Name: "duplicate", Description: " "},
	}

	err := validateShardDefinitions(shards)
	require.ErrorIs(t, err, errEmptyShardName)
	require.ErrorIs(t, err, errEmptyShardQuery)
	require.ErrorIs(t, err, errEmptyShardDescription)
	require.ErrorIs(t, err, errDuplicateShardName)
}

func TestGenerateSuiteFile(t *testing.T) {
	shards := []shard{
		{Name: "b", Description: "B tests.\nMore B tests.", Query: "query-b"},
		{Name: "a", Description: "A tests.", Query: "query-a"},
	}
	testsByShard := map[string][]queryTarget{
		"a": goTestTargets("//pkg:a_test"),
		"b": goTestTargets("//pkg:b_test"),
	}

	var allTests []queryTarget
	for _, shard := range shards {
		allTests = append(allTests, testsByShard[shard.Name]...)
	}

	var queries []string
	runQuery := func(query string) ([]queryTarget, error) {
		queries = append(queries, query)
		if query == allRunnableGoTestsQuery() {
			return allTests, nil
		}
		for _, shard := range shards {
			if query == runnableTestsQuery(shard.Query) {
				return testsByShard[shard.Name], nil
			}
		}
		return nil, fmt.Errorf("unexpected query %q", query)
	}

	got, err := generateSuiteFile(shards, runQuery)
	require.NoError(t, err)

	want := strings.Join([]string{
		"\"\"\"Generated unit-test shard suites. Do not edit by hand.",
		"",
		"Regenerate with: task bazel-generate-unit-test-suites",
		"\"\"\"",
		"",
		"UNIT_TEST_SHARDS = {",
		"    # B tests.",
		"    # More B tests.",
		"    \"b\": [",
		"        \"//pkg:b_test\",",
		"    ],",
		"    # A tests.",
		"    \"a\": [",
		"        \"//pkg:a_test\",",
		"    ],",
		"}",
		"",
	}, "\n")
	require.Equal(t, want, got)

	var wantQueries []string
	for _, shard := range shards {
		wantQueries = append(wantQueries, runnableTestsQuery(shard.Query))
	}
	wantQueries = append(wantQueries, allRunnableGoTestsQuery())
	require.Equal(t, wantQueries, queries)
}

type queryResponse struct {
	targets []queryTarget
	err     error
}

func queryRunnerFor(responses map[string]queryResponse) queryRunner {
	return func(query string) ([]queryTarget, error) {
		response, ok := responses[query]
		if !ok {
			return nil, fmt.Errorf("unexpected query %q", query)
		}
		return response.targets, response.err
	}
}

func TestGenerateSuiteFileErrors(t *testing.T) {
	queryErr := errors.New("query failed")
	unitTestShard := []shard{{Name: "avalanchego_unit_tests", Description: "AvalancheGo unit tests.", Query: "tests(//...)"}}
	shardsWithOverlap := []shard{
		{Name: "first", Description: "First tests.", Query: "query-first"},
		{Name: "second", Description: "Second tests.", Query: "query-second"},
	}
	tests := []struct {
		name      string
		shards    []shard
		responses map[string]queryResponse
		wantErrs  []error
	}{
		{
			name:   "empty shard",
			shards: unitTestShard,
			responses: map[string]queryResponse{
				runnableTestsQuery("tests(//...)"): {},
			},
			wantErrs: []error{errEmptyShard},
		},
		{
			name: "unassigned test",
			shards: []shard{{
				Name:        "avalanchego_unit_tests",
				Description: "AvalancheGo unit tests.",
				Query:       "query-avalanchego",
			}},
			responses: map[string]queryResponse{
				runnableTestsQuery("query-avalanchego"): {targets: goTestTargets("//pkg:assigned_test")},
				allRunnableGoTestsQuery():               {targets: goTestTargets("//pkg:assigned_test", "//pkg:unassigned_test")},
			},
			wantErrs: []error{errUnassignedTests},
		},
		{
			name:   "overlapping tests",
			shards: shardsWithOverlap,
			responses: map[string]queryResponse{
				runnableTestsQuery("query-first"):  {targets: goTestTargets("//pkg:shared_test")},
				runnableTestsQuery("query-second"): {targets: goTestTargets("//pkg:shared_test")},
				allRunnableGoTestsQuery():          {targets: goTestTargets("//pkg:shared_test")},
			},
			wantErrs: []error{errOverlappingTests},
		},
		{
			name:   "unassigned and overlapping tests",
			shards: shardsWithOverlap,
			responses: map[string]queryResponse{
				runnableTestsQuery("query-first"):  {targets: goTestTargets("//pkg:shared_test")},
				runnableTestsQuery("query-second"): {targets: goTestTargets("//pkg:shared_test")},
				allRunnableGoTestsQuery():          {targets: goTestTargets("//pkg:shared_test", "//pkg:unassigned_test")},
			},
			wantErrs: []error{errUnassignedTests, errOverlappingTests},
		},
		{
			name:   "overlapping test outside all tests query",
			shards: shardsWithOverlap,
			responses: map[string]queryResponse{
				runnableTestsQuery("query-first"):  {targets: goTestTargets("@external//pkg:shared_test")},
				runnableTestsQuery("query-second"): {targets: goTestTargets("@external//pkg:shared_test")},
				allRunnableGoTestsQuery():          {},
			},
			wantErrs: []error{errOverlappingTests},
		},
		{
			name:   "non-Go target",
			shards: unitTestShard,
			responses: map[string]queryResponse{
				runnableTestsQuery("tests(//...)"): {targets: []queryTarget{{kind: "sh_test rule", label: "//pkg:some_non_go_test"}}},
			},
			wantErrs: []error{errNonGoTests},
		},
		{
			name:   "query error",
			shards: unitTestShard,
			responses: map[string]queryResponse{
				runnableTestsQuery("tests(//...)"): {err: queryErr},
			},
			wantErrs: []error{queryErr},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := generateSuiteFile(tt.shards, queryRunnerFor(tt.responses))
			for _, wantErr := range tt.wantErrs {
				require.ErrorIs(t, err, wantErr)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	const (
		shardsPath = "/repo/.bazel/test_shards.json"
		outputPath = "/repo/.bazel/generated_test_suites.bzl"
	)
	contents := []byte(`{"shards":[{"name":"unit_tests","description":"Unit tests.","query":"tests(//...)"}]}`)

	var gotPath string
	var gotPerm os.FileMode
	err := generate(
		shardsPath,
		outputPath,
		func(path string) ([]byte, error) {
			require.Equal(t, shardsPath, path)
			return contents, nil
		},
		func(path string, _ []byte, perm os.FileMode) error {
			gotPath = path
			gotPerm = perm
			return nil
		},
		func(query string) ([]queryTarget, error) {
			switch query {
			case runnableTestsQuery("tests(//...)"), allRunnableGoTestsQuery():
				return goTestTargets("//pkg:unit_test"), nil
			default:
				return nil, fmt.Errorf("unexpected query %q", query)
			}
		},
	)
	require.NoError(t, err)
	require.Equal(t, outputPath, gotPath)
	require.Equal(t, os.FileMode(0o644), gotPerm)
}

func TestParseQueryTargets(t *testing.T) {
	output := "sh_test rule //pkg:script_test\ngo_test rule //pkg:go_test\n"

	targets, err := parseQueryTargets(output)
	require.NoError(t, err)
	require.Equal(t, []queryTarget{
		{kind: "go_test rule", label: "//pkg:go_test"},
		{kind: "sh_test rule", label: "//pkg:script_test"},
	}, targets)
}

func TestSortedNonEmptyLines(t *testing.T) {
	got := sortedNonEmptyLines("\n//b:test\n\n//a:test\n")
	want := []string{"//a:test", "//b:test"}
	require.Equal(t, want, got)
}
