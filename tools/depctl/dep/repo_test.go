// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dep

import "testing"

func TestRepoTargetResolve(t *testing.T) {
	tests := []struct {
		name           string
		target         RepoTarget
		wantModulePath string
		wantCloneURL   string
		wantErr        bool
	}{
		{
			name:           "avalanchego",
			target:         TargetAvalanchego,
			wantModulePath: "github.com/ava-labs/avalanchego",
			wantCloneURL:   "https://github.com/ava-labs/avalanchego",
			wantErr:        false,
		},
		{
			name:           "firewood",
			target:         TargetFirewood,
			wantModulePath: "github.com/ava-labs/firewood-go-ethhash/ffi",
			wantCloneURL:   "https://github.com/ava-labs/firewood",
			wantErr:        false,
		},
		{
			name:           "coreth",
			target:         TargetCoreth,
			wantModulePath: "github.com/ava-labs/coreth",
			wantCloneURL:   "https://github.com/ava-labs/coreth",
			wantErr:        false,
		},
		{
			name:           "invalid target",
			target:         RepoTarget("invalid"),
			wantModulePath: "",
			wantCloneURL:   "",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modulePath, cloneURL, err := tt.target.Resolve()
			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if modulePath != tt.wantModulePath {
				t.Errorf("Resolve() modulePath = %v, want %v", modulePath, tt.wantModulePath)
			}
			if cloneURL != tt.wantCloneURL {
				t.Errorf("Resolve() cloneURL = %v, want %v", cloneURL, tt.wantCloneURL)
			}
		})
	}
}

func TestParseRepoTarget(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  RepoTarget
	}{
		{
			name:  "empty string defaults to avalanchego",
			input: "",
			want:  TargetAvalanchego,
		},
		{
			name:  "avalanchego",
			input: "avalanchego",
			want:  TargetAvalanchego,
		},
		{
			name:  "firewood",
			input: "firewood",
			want:  TargetFirewood,
		},
		{
			name:  "coreth",
			input: "coreth",
			want:  TargetCoreth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRepoTarget(tt.input)
			if got != tt.want {
				t.Errorf("ParseRepoTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRepoTargetIsValid(t *testing.T) {
	tests := []struct {
		name   string
		target RepoTarget
		want   bool
	}{
		{
			name:   "avalanchego is valid",
			target: TargetAvalanchego,
			want:   true,
		},
		{
			name:   "firewood is valid",
			target: TargetFirewood,
			want:   true,
		},
		{
			name:   "coreth is valid",
			target: TargetCoreth,
			want:   true,
		},
		{
			name:   "invalid target",
			target: RepoTarget("invalid"),
			want:   false,
		},
		{
			name:   "empty target",
			target: RepoTarget(""),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.target.IsValid()
			if got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRepoTargetString(t *testing.T) {
	tests := []struct {
		name   string
		target RepoTarget
		want   string
	}{
		{
			name:   "avalanchego",
			target: TargetAvalanchego,
			want:   "avalanchego",
		},
		{
			name:   "firewood",
			target: TargetFirewood,
			want:   "firewood",
		},
		{
			name:   "coreth",
			target: TargetCoreth,
			want:   "coreth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.target.String()
			if got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}
