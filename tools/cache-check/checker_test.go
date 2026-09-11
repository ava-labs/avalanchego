// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		checks  []string
		wantErr error
	}{
		{
			name:    "Task cache restore",
			checks:  []string{taskCacheRestoreCheck},
			wantErr: errCheckUnimplemented,
		},
		{
			name:    "module cache restore",
			checks:  []string{goModCacheRestoreCheck},
			wantErr: errCheckUnimplemented,
		},
		{
			name:    "unit cache restore",
			checks:  []string{goUnitCacheRestoreCheck},
			wantErr: errCheckUnimplemented,
		},
		{
			name:    "unit test results",
			checks:  []string{goUnitTestResultsCheck},
			wantErr: errCheckUnimplemented,
		},
		{
			name:    "unknown",
			checks:  []string{"unknown"},
			wantErr: errUnknownCheck,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := check([]byte("job output"), test.checks)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestCheckReportsAllFailures(t *testing.T) {
	err := check([]byte("job output"), []string{
		taskCacheRestoreCheck,
		goModCacheRestoreCheck,
		goUnitCacheRestoreCheck,
		goUnitTestResultsCheck,
	})

	require.ErrorIs(t, err, errCheckUnimplemented)
	require.Equal(t, "cache check is not implemented: \"task-cache-restore\"\ncache check is not implemented: \"go-mod-cache-restore\"\ncache check is not implemented: \"go-unit-cache-restore\"\ncache check is not implemented: \"go-unit-test-results\"", err.Error())
}
