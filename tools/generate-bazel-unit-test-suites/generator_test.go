// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestGenerateSuiteFile(t *testing.T) {
	shards := []shard{
		{Name: "b", Description: "B tests.\nMore B tests.", Query: "query-b"},
		{Name: "a", Description: "A tests.", Query: "query-a"},
	}

	queries := []string{}
	runQuery := func(query string) ([]string, error) {
		queries = append(queries, query)
		switch query {
		case nonGoTestsQuery("query-b"), nonGoTestsQuery("query-a"):
			return nil, nil
		case runnableTestsQuery("query-b"):
			return []string{"//pkg:b_test"}, nil
		case runnableTestsQuery("query-a"):
			return []string{"//pkg:a_test"}, nil
		case allRunnableGoTestsQuery():
			return []string{"//pkg:a_test", "//pkg:b_test"}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", query)
		}
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

	wantQueries := []string{
		nonGoTestsQuery("query-b"),
		runnableTestsQuery("query-b"),
		nonGoTestsQuery("query-a"),
		runnableTestsQuery("query-a"),
		allRunnableGoTestsQuery(),
	}
	require.Equal(t, wantQueries, queries)
}

func TestNonGoTestsQueryExcludesManualTargets(t *testing.T) {
	query := "tests(//...)"
	want := fmt.Sprintf(
		"(%s) except kind(\"go_test rule\", (%s))",
		runnableTestsQuery(query),
		runnableTestsQuery(query),
	)
	require.Equal(t, want, nonGoTestsQuery(query))
}

func TestGenerateSuiteFileRejectsEmptyShard(t *testing.T) {
	shards := []shard{{Name: "avalanchego_unit_tests", Description: "AvalancheGo unit tests.", Query: "tests(//...)"}}

	runQuery := func(string) ([]string, error) {
		return nil, nil
	}

	_, err := generateSuiteFile(shards, runQuery)
	require.ErrorIs(t, err, errEmptyShard)
}

func TestGenerateSuiteFileRejectsUnassignedTest(t *testing.T) {
	shards := []shard{{Name: "avalanchego_unit_tests", Description: "AvalancheGo unit tests.", Query: "query-avalanchego"}}

	runQuery := func(query string) ([]string, error) {
		switch query {
		case nonGoTestsQuery("query-avalanchego"):
			return nil, nil
		case runnableTestsQuery("query-avalanchego"):
			return []string{"//pkg:assigned_test"}, nil
		case allRunnableGoTestsQuery():
			return []string{"//pkg:assigned_test", "//pkg:unassigned_test"}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", query)
		}
	}

	_, err := generateSuiteFile(shards, runQuery)
	require.ErrorIs(t, err, errUnassignedTests)
}

func TestGenerateSuiteFileRejectsOverlappingTests(t *testing.T) {
	shards := []shard{
		{Name: "first", Description: "First tests.", Query: "query-first"},
		{Name: "second", Description: "Second tests.", Query: "query-second"},
	}

	runQuery := func(query string) ([]string, error) {
		switch query {
		case nonGoTestsQuery("query-first"), nonGoTestsQuery("query-second"):
			return nil, nil
		case runnableTestsQuery("query-first"), runnableTestsQuery("query-second"), allRunnableGoTestsQuery():
			return []string{"//pkg:shared_test"}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", query)
		}
	}

	_, err := generateSuiteFile(shards, runQuery)
	require.ErrorIs(t, err, errOverlappingTests)
}

func TestGenerateSuiteFileRejectsNonGoTargets(t *testing.T) {
	shards := []shard{{Name: "avalanchego_unit_tests", Description: "AvalancheGo unit tests.", Query: "tests(//...)"}}

	runQuery := func(query string) ([]string, error) {
		if query == nonGoTestsQuery("tests(//...)") {
			return []string{"//pkg:some_non_go_test"}, nil
		}
		return nil, nil
	}

	_, err := generateSuiteFile(shards, runQuery)
	require.ErrorIs(t, err, errNonGoTests)
}

func TestSortedNonEmptyLines(t *testing.T) {
	got := sortedNonEmptyLines("\n//b:test\n\n//a:test\n")
	want := []string{"//a:test", "//b:test"}
	require.Equal(t, want, got)
}
