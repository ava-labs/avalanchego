// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name                        string
		fixture                     string
		checks                      []string
		allowUncachedGoTestPackages []string
		wantErr                     error
	}{
		{
			name:    "cache hit",
			fixture: "cache-hit.txt",
			checks:  knownChecks,
		},
		{
			name:    "Task cache miss",
			fixture: "cache-miss.txt",
			checks:  []string{taskCacheRestoreCheck},
			wantErr: errCacheRestoreMiss,
		},
		{
			name:    "cache restore evidence must be exact",
			fixture: "cache-not-exact.txt",
			checks:  []string{taskCacheRestoreCheck},
			wantErr: errCacheRestoreMiss,
		},
		{
			name:    "module cache miss",
			fixture: "cache-miss.txt",
			checks:  []string{goModCacheRestoreCheck},
			wantErr: errCacheRestoreMiss,
		},
		{
			name:    "unit cache miss",
			fixture: "cache-miss.txt",
			checks:  []string{goUnitCacheRestoreCheck},
			wantErr: errCacheRestoreMiss,
		},
		{
			name:    "module download",
			fixture: "module-download.txt",
			checks:  []string{goModCacheRestoreCheck},
			wantErr: errGoModuleDownload,
		},
		{
			name:    "uncached test result",
			fixture: "uncached-go-test.txt",
			checks:  []string{goUnitTestResultsCheck},
			wantErr: errGoTestResultNotCached,
		},
		{
			name:                        "allowed uncached test result",
			fixture:                     "uncached-go-test.txt",
			checks:                      []string{goUnitTestResultsCheck},
			allowUncachedGoTestPackages: []string{"github.com/ava-labs/avalanchego/tools/cache-check"},
		},
		{
			name:    "missing test result",
			fixture: "no-go-test-result.txt",
			checks:  []string{goUnitTestResultsCheck},
			wantErr: errGoTestResultMissing,
		},
		{
			name:    "unknown",
			fixture: "cache-hit.txt",
			checks:  []string{"unknown"},
			wantErr: errUnknownCheck,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := readFixture(t, test.fixture)
			err := checkWithOptions(log, test.checks, checkOptions{
				allowUncachedGoTestPackages: test.allowUncachedGoTestPackages,
			})
			if test.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestCheckReportsAllFailures(t *testing.T) {
	err := check(readFixture(t, "cache-miss.txt"), knownChecks)

	require.ErrorIs(t, err, errCacheRestoreMiss)
	require.ErrorIs(t, err, errGoTestResultMissing)
	require.Equal(t, "task-cache-restore: cache was not restored exactly: task-cache-hit=true\ngo-mod-cache-restore: cache was not restored exactly: go-mod-cache-hit=true\ngo-unit-cache-restore: cache was not restored exactly: go-unit-cache-hit=true\ngo-unit-test-results: go test result is missing", err.Error())
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	log, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return log
}
