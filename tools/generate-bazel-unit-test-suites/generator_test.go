// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseShardFile(t *testing.T) {
	contents := []byte(`{"shards":[{"name":"main_unit_tests","description":"Main shard","query":"tests(//...)"}]}`)

	shards, err := parseShardFile(contents)
	if err != nil {
		t.Fatalf("parseShardFile returned error: %v", err)
	}

	expected := []shard{{
		Name:        "main_unit_tests",
		Description: "Main shard",
		Query:       "tests(//...)",
	}}
	if !reflect.DeepEqual(expected, shards) {
		t.Fatalf("unexpected shards: got %#v want %#v", shards, expected)
	}
}

func TestValidateShardDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		shards  []shard
		wantErr string
	}{
		{
			name: "valid",
			shards: []shard{{
				Name:  "main_unit_tests",
				Query: "tests(//...)",
			}},
		},
		{
			name: "empty name",
			shards: []shard{{
				Query: "tests(//...)",
			}},
			wantErr: "empty name",
		},
		{
			name: "empty query",
			shards: []shard{{
				Name: "main_unit_tests",
			}},
			wantErr: "empty query",
		},
		{
			name: "duplicate name",
			shards: []shard{
				{Name: "main_unit_tests", Query: "tests(//... )"},
				{Name: "main_unit_tests", Query: "tests(//foo/...)"},
			},
			wantErr: "duplicate shard name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShardDefinitions(tt.shards)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateShardDefinitions returned error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateShardDefinitions error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateSuiteFile(t *testing.T) {
	shards := []shard{
		{Name: "b", Query: "query-b"},
		{Name: "a", Query: "query-a"},
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
		default:
			return nil, fmt.Errorf("unexpected query %q", query)
		}
	}

	got, err := generateSuiteFile(shards, runQuery)
	if err != nil {
		t.Fatalf("generateSuiteFile returned error: %v", err)
	}

	want := strings.Join([]string{
		"\"\"\"Generated unit-test shard suites. Do not edit by hand.",
		"",
		"Regenerate with: task bazel-generate-unit-test-suites",
		"\"\"\"",
		"",
		"UNIT_TEST_SHARDS = {",
		"    \"b\": [",
		"        \"//pkg:b_test\",",
		"    ],",
		"    \"a\": [",
		"        \"//pkg:a_test\",",
		"    ],",
		"}",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("generateSuiteFile output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	wantQueries := []string{
		nonGoTestsQuery("query-b"),
		runnableTestsQuery("query-b"),
		nonGoTestsQuery("query-a"),
		runnableTestsQuery("query-a"),
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", queries, wantQueries)
	}
}

func TestGenerateSuiteFileRejectsNonGoTargets(t *testing.T) {
	shards := []shard{{Name: "main_unit_tests", Query: "tests(//...)"}}

	runQuery := func(query string) ([]string, error) {
		if query == nonGoTestsQuery("tests(//...)") {
			return []string{"//pkg:some_non_go_test"}, nil
		}
		return nil, nil
	}

	_, err := generateSuiteFile(shards, runQuery)
	if err == nil {
		t.Fatal("generateSuiteFile returned nil error, want non-Go target failure")
	}
	if !strings.Contains(err.Error(), "non-Go test targets") {
		t.Fatalf("error = %q, want non-Go target message", err)
	}
	if !strings.Contains(err.Error(), "//pkg:some_non_go_test") {
		t.Fatalf("error = %q, want failing target", err)
	}
}

func TestSortedNonEmptyLines(t *testing.T) {
	got := sortedNonEmptyLines("\n//b:test\n\n//a:test\n")
	want := []string{"//a:test", "//b:test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedNonEmptyLines = %#v, want %#v", got, want)
	}
}
