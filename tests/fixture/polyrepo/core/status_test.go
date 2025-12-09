// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// TestStatus_FromAvalanchego tests Status() when running from avalanchego directory
func TestStatus_FromAvalanchego(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create temp directory with avalanchego go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(`module github.com/ava-labs/avalanchego
go 1.24
`), 0o600))

	// Call Status() with bytes.Buffer to capture output
	var buf bytes.Buffer
	require.NoError(Status(log, tmpDir, &buf))

	// Verify output format
	output := buf.String()
	require.Contains(output, "Primary Repository: avalanchego")
	require.Contains(output, "Other Repositories:")
	require.Contains(output, "firewood: not cloned")

	// The primary repo section should show avalanchego status (may be "not cloned" without .git)
	// but avalanchego should NOT appear in the "Other Repositories" section
	lines := strings.Split(output, "\n")
	inOtherRepos := false
	for _, line := range lines {
		if strings.Contains(line, "Other Repositories:") {
			inOtherRepos = true
			continue
		}
		if inOtherRepos && strings.Contains(line, "avalanchego:") {
			require.Fail("avalanchego should not appear in Other Repositories section")
		}
	}

	// Verify coreth does not appear anywhere (no longer a managed repo)
	require.NotContains(output, "coreth:")
}

// TestStatus_FromUnknownLocation tests Status() when not in a known repository
func TestStatus_FromUnknownLocation(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create temp directory with NO go.mod
	tmpDir := t.TempDir()

	// Call Status()
	var buf bytes.Buffer
	require.NoError(Status(log, tmpDir, &buf))

	// Verify output format
	output := buf.String()
	require.Contains(output, "Primary Repository: none (not in a known repository)")
	require.Contains(output, "Other Repositories:")
	require.Contains(output, "avalanchego: not cloned")
	require.Contains(output, "firewood: not cloned")
}

// TestStatus_WithClonedRepos tests Status() when some repos are cloned
// Note: This is a basic unit test. Full integration with real cloned repos
// is tested in status_integration_test.go
func TestStatus_WithClonedRepos(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create temp directory with avalanchego go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(`module github.com/ava-labs/avalanchego
go 1.24
`), 0o600))

	// Create a fake firewood directory (just the directory structure, no git)
	// This tests that Status() attempts to check synced repo directories
	firewoodPath := filepath.Join(tmpDir, "firewood")
	require.NoError(os.MkdirAll(firewoodPath, 0o755))

	// Call Status()
	var buf bytes.Buffer
	require.NoError(Status(log, tmpDir, &buf))

	// Verify output structure - firewood directory exists but not a git repo
	output := buf.String()
	require.Contains(output, "Primary Repository: avalanchego")
	require.Contains(output, "Other Repositories:")
	// firewood directory exists but won't show as cloned without .git
	require.Contains(output, "firewood:")
}

// TestStatus_OutputFormat tests that Status() produces correct output structure
func TestStatus_OutputFormat(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create temp directory with avalanchego go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(`module github.com/ava-labs/avalanchego
go 1.24
`), 0o600))

	// Call Status()
	var buf bytes.Buffer
	require.NoError(Status(log, tmpDir, &buf))

	// Verify output structure
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// First section should be Primary Repository
	require.True(strings.HasPrefix(lines[0], "Primary Repository:"))

	// Should have "Other Repositories:" section
	foundOthers := false
	for _, line := range lines {
		if strings.Contains(line, "Other Repositories:") {
			foundOthers = true
			break
		}
	}
	require.True(foundOthers, "Output should contain 'Other Repositories:' section")
}

// TestStatus_FromFirewood tests Status() when running from firewood directory
func TestStatus_FromFirewood(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create temp directory with firewood's special ffi/go.mod structure
	tmpDir := t.TempDir()
	ffiDir := filepath.Join(tmpDir, "ffi")
	require.NoError(os.MkdirAll(ffiDir, 0o755))

	goModPath := filepath.Join(ffiDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(`module github.com/ava-labs/firewood/ffi
go 1.24
`), 0o600))

	// Call Status()
	var buf bytes.Buffer
	require.NoError(Status(log, tmpDir, &buf))

	// Verify output
	output := buf.String()
	require.Contains(output, "Primary Repository: firewood")
	require.Contains(output, "Other Repositories:")
	require.Contains(output, "avalanchego: not cloned")

	// Firewood should NOT appear in the "Other Repositories" section
	lines := strings.Split(output, "\n")
	inOtherRepos := false
	for _, line := range lines {
		if strings.Contains(line, "Other Repositories:") {
			inOtherRepos = true
			continue
		}
		if inOtherRepos && strings.Contains(line, "firewood:") {
			require.Fail("firewood should not appear in Other Repositories section")
		}
	}
}

// TestStatus_WithGoModInBaseDir tests Status() when go.mod exists but no primary repo detected
func TestStatus_WithGoModInBaseDir(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create temp directory with a go.mod that's NOT from a known repo
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(`module example.com/unknown
go 1.24
`), 0o600))

	// Call Status()
	var buf bytes.Buffer
	require.NoError(Status(log, tmpDir, &buf))

	// Verify output - should show no primary repo but still check for synced repos
	output := buf.String()
	require.Contains(output, "Primary Repository: none (not in a known repository)")
	require.Contains(output, "Other Repositories:")
}
