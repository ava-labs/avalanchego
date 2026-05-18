// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScopeExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		scopes    []string
		want      string
		wantError string
	}{
		{
			name:   "single positive scope",
			scopes: []string{"//graft/subnet-evm/..."},
			want:   "//graft/subnet-evm/...",
		},
		{
			name:   "multiple positive scopes",
			scopes: []string{"//graft/coreth/...", "//graft/evm/..."},
			want:   "//graft/coreth/... union //graft/evm/...",
		},
		{
			name:   "positive and negative scopes",
			scopes: []string{"//...", "-//graft/..."},
			want:   "//... except //graft/...",
		},
		{
			name:      "no scopes",
			scopes:    nil,
			wantError: "at least one --scope is required",
		},
		{
			name:      "only negative scopes",
			scopes:    []string{"-//graft/..."},
			wantError: "at least one positive --scope is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := scopeExpression(tt.scopes)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFilterManifest(t *testing.T) {
	t.Parallel()

	impacted := []string{
		"//utils:go_default_library",
		"//utils:go_default_test",
		"//network/p2p:go_default_test",
	}
	partitionTests := []string{
		"//utils:go_default_test",
		"//foo:go_default_test",
	}

	require.Equal(t, []string{"//utils:go_default_test"}, filterManifest(impacted, partitionTests))
}

func TestFormatManifest(t *testing.T) {
	t.Parallel()

	require.Equal(t, "//a:test\n//b:test\n", formatManifest([]string{"//a:test", "//b:test"}))
	require.Empty(t, formatManifest(nil))
}
