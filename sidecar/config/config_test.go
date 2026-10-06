// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sidecar.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantErr      error
		wantTypes    []string
		wantBindAddr string
	}{
		{
			name: "happy path single verifier",
			content: `{
				"bind_addr": ":9900",
				"verifiers": {
					"solana": {"rpc_url": "https://api.devnet.solana.com"}
				}
			}`,
			wantTypes:    []string{"solana"},
			wantBindAddr: ":9900",
		},
		{
			name: "multiple verifiers",
			content: `{
				"bind_addr": ":9900",
				"verifiers": {
					"solana":   {"rpc_url": "x"},
					"polkadot": {"rpc_url": "y"}
				}
			}`,
			wantTypes:    []string{"polkadot", "solana"},
			wantBindAddr: ":9900",
		},
		{
			name:    "malformed json",
			content: `{ this is not json`,
			wantErr: ErrParse,
		},
		{
			name:    "empty verifiers map",
			content: `{"bind_addr": ":9900", "verifiers": {}}`,
			wantErr: ErrNoVerifiers,
		},
		{
			name:    "missing verifiers field",
			content: `{"bind_addr": ":9900"}`,
			wantErr: ErrNoVerifiers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.content)
			cfg, err := Load(path)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantBindAddr, cfg.BindAddr)
			got := cfg.SourceTypes()
			slices.Sort(got)
			require.Equal(t, tt.wantTypes, got)
		})
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	require.ErrorIs(t, err, ErrRead)
}
