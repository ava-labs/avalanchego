// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"
)

// Test that we can get config for avalanchego
func TestGetRepoConfig_Avalanchego(t *testing.T) {
	config, err := GetRepoConfig("avalanchego")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config.Name != "avalanchego" {
		t.Errorf("expected name 'avalanchego', got '%s'", config.Name)
	}

	if config.GoModule != "github.com/ava-labs/avalanchego" {
		t.Errorf("expected module 'github.com/ava-labs/avalanchego', got '%s'", config.GoModule)
	}

	if config.DefaultBranch != "master" {
		t.Errorf("expected default branch 'master', got '%s'", config.DefaultBranch)
	}

	if config.ModuleReplacementPath != "." {
		t.Errorf("expected replacement path '.', got '%s'", config.ModuleReplacementPath)
	}

	if config.RequiresNixBuild {
		t.Errorf("expected RequiresNixBuild to be false")
	}
}

// Test that we can get config for coreth
func TestGetRepoConfig_Coreth(t *testing.T) {
	config, err := GetRepoConfig("coreth")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config.Name != "coreth" {
		t.Errorf("expected name 'coreth', got '%s'", config.Name)
	}

	if config.GoModule != "github.com/ava-labs/coreth" {
		t.Errorf("expected module 'github.com/ava-labs/coreth', got '%s'", config.GoModule)
	}

	if config.DefaultBranch != "master" {
		t.Errorf("expected default branch 'master', got '%s'", config.DefaultBranch)
	}

	if config.RequiresNixBuild {
		t.Errorf("expected RequiresNixBuild to be false")
	}
}

// Test that we can get config for firewood
func TestGetRepoConfig_Firewood(t *testing.T) {
	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config.Name != "firewood" {
		t.Errorf("expected name 'firewood', got '%s'", config.Name)
	}

	if config.GoModule != "github.com/ava-labs/firewood/ffi" {
		t.Errorf("expected module 'github.com/ava-labs/firewood/ffi', got '%s'", config.GoModule)
	}

	if config.DefaultBranch != "main" {
		t.Errorf("expected default branch 'main', got '%s'", config.DefaultBranch)
	}

	if config.ModuleReplacementPath != "./ffi/result/ffi" {
		t.Errorf("expected replacement path './ffi/result/ffi', got '%s'", config.ModuleReplacementPath)
	}

	if !config.RequiresNixBuild {
		t.Errorf("expected RequiresNixBuild to be true")
	}

	if config.NixBuildPath != "ffi" {
		t.Errorf("expected NixBuildPath 'ffi', got '%s'", config.NixBuildPath)
	}
}

// Test that we get an error for unknown repos
func TestGetRepoConfig_Unknown(t *testing.T) {
	_, err := GetRepoConfig("unknown")
	if err == nil {
		t.Fatal("expected error for unknown repo, got nil")
	}
}

// Test that we can get all repo configs
func TestGetAllRepoConfigs(t *testing.T) {
	configs := GetAllRepoConfigs()
	if len(configs) != 3 {
		t.Fatalf("expected 3 configs, got %d", len(configs))
	}

	// Verify we have all three repos
	names := make(map[string]bool)
	for _, config := range configs {
		names[config.Name] = true
	}

	if !names["avalanchego"] {
		t.Error("missing avalanchego config")
	}
	if !names["coreth"] {
		t.Error("missing coreth config")
	}
	if !names["firewood"] {
		t.Error("missing firewood config")
	}
}
