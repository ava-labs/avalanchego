// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dep

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPseudoVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{
			name:    "pseudo-version with v0.0.0",
			version: "v0.0.0-20240101120000-abcdef123456",
			want:    true,
		},
		{
			name:    "pseudo-version with v1.2.3",
			version: "v1.2.3-20240101120000-abcdef123456",
			want:    true,
		},
		{
			name:    "tagged version v1.2.3",
			version: "v1.2.3",
			want:    false,
		},
		{
			name:    "tagged version with pre-release",
			version: "v1.2.3-rc1",
			want:    false,
		},
		{
			name:    "tagged version with build metadata",
			version: "v1.2.3+build123",
			want:    false,
		},
		{
			name:    "invalid pseudo-version missing timestamp",
			version: "v0.0.0-abcdef123456",
			want:    false,
		},
		{
			name:    "invalid pseudo-version timestamp too short",
			version: "v0.0.0-2024010112-abcdef123456",
			want:    false,
		},
		{
			name:    "invalid pseudo-version timestamp has letters",
			version: "v0.0.0-2024010112000a-abcdef123456",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPseudoVersion(tt.version)
			if got != tt.want {
				t.Errorf("isPseudoVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetVersion(t *testing.T) {
	tests := []struct {
		name       string
		target     RepoTarget
		goModData  string
		wantVer    string
		wantErr    bool
		errContain string
	}{
		{
			name:   "tagged version",
			target: TargetAvalanchego,
			goModData: `module github.com/ava-labs/test

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.11
)
`,
			wantVer: "v1.11.11",
			wantErr: false,
		},
		{
			name:   "pseudo-version",
			target: TargetCoreth,
			goModData: `module github.com/ava-labs/test

go 1.21

require (
	github.com/ava-labs/coreth v0.0.0-20240101120000-abcdef123456
)
`,
			wantVer: "abcdef12",
			wantErr: false,
		},
		{
			name:   "pseudo-version short hash",
			target: TargetFirewood,
			goModData: `module github.com/ava-labs/test

go 1.21

require (
	github.com/ava-labs/firewood-go-ethhash/ffi v0.0.0-20240101120000-abc123
)
`,
			wantVer: "abc123",
			wantErr: false,
		},
		{
			name:   "module not found",
			target: TargetAvalanchego,
			goModData: `module github.com/ava-labs/test

go 1.21

require (
	github.com/ava-labs/coreth v1.2.3
)
`,
			wantVer:    "",
			wantErr:    true,
			errContain: "not found in go.mod",
		},
		{
			name:   "invalid target",
			target: RepoTarget("invalid"),
			goModData: `module github.com/ava-labs/test

go 1.21
`,
			wantVer:    "",
			wantErr:    true,
			errContain: "invalid repo target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory with go.mod
			tmpDir := t.TempDir()
			goModPath := filepath.Join(tmpDir, "go.mod")
			if err := os.WriteFile(goModPath, []byte(tt.goModData), 0o644); err != nil {
				t.Fatalf("failed to write test go.mod: %v", err)
			}

			// Change to temp directory
			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("failed to get working directory: %v", err)
			}
			defer func() {
				if err := os.Chdir(oldWd); err != nil {
					t.Fatalf("failed to restore working directory: %v", err)
				}
			}()
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("failed to change to temp directory: %v", err)
			}

			// Test GetVersion
			gotVer, err := GetVersion(tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContain != "" {
				if err == nil || !contains(err.Error(), tt.errContain) {
					t.Errorf("GetVersion() error = %v, should contain %q", err, tt.errContain)
				}
			}
			if gotVer != tt.wantVer {
				t.Errorf("GetVersion() = %v, want %v", gotVer, tt.wantVer)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && hasSubstring(s, substr)))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
