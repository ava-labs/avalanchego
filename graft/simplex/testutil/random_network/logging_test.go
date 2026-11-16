// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

func TestFileLogging(t *testing.T) {
	// Create temp directory for test logs
	tempDir := t.TempDir()

	// Create config with file logging enabled
	config := DefaultFuzzConfig()
	config.LogDirectory = tempDir

	// Create network logger
	networkLogger := CreateNetworkLogger(t, config)
	networkLogger.Info("Test network log message")

	// Create node logger
	nodeID := testutil.GenerateNodeID(t)
	nodeLogger := CreateNodeLogger(t, config, nodeID)
	nodeLogger.Info("Test node log message")

	// Verify main.log was created and contains the message
	mainLogPath := filepath.Join(tempDir, "main.log")
	require.FileExists(t, mainLogPath)

	mainLogContent, err := os.ReadFile(mainLogPath)
	require.NoError(t, err)
	require.Contains(t, string(mainLogContent), "Test network log message")

	// Verify node log was created with hex filename
	nodeLogPattern := filepath.Join(tempDir, "*.log")
	matches, err := filepath.Glob(nodeLogPattern)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(matches), 2) // Should have main.log and at least one node log

	// Find the node log file (not main.log)
	var nodeLogPath string
	for _, match := range matches {
		if filepath.Base(match) != "main.log" {
			nodeLogPath = match
			break
		}
	}
	require.NotEmpty(t, nodeLogPath)

	nodeLogContent, err := os.ReadFile(nodeLogPath)
	require.NoError(t, err)
	require.Contains(t, string(nodeLogContent), "Test node log message")
}

func TestLogDirectoryClearing(t *testing.T) {
	// Create temp directory for test logs
	tempDir := t.TempDir()

	// Create an old log file
	oldLogPath := filepath.Join(tempDir, "old.log")
	err := os.WriteFile(oldLogPath, []byte("old log content"), 0644)
	require.NoError(t, err)
	require.FileExists(t, oldLogPath)

	// Create config with file logging enabled
	config := DefaultFuzzConfig()
	config.LogDirectory = tempDir

	// Create network logger - this should clear the directory
	networkLogger := CreateNetworkLogger(t, config)
	networkLogger.Info("New log message")

	// Verify old log was removed
	require.NoFileExists(t, oldLogPath)

	// Verify new main.log was created
	mainLogPath := filepath.Join(tempDir, "main.log")
	require.FileExists(t, mainLogPath)
}

func TestConsoleOnlyLogging(t *testing.T) {
	// Create config with file logging disabled (empty directory)
	config := DefaultFuzzConfig()
	config.LogDirectory = ""

	// Create network logger - should not panic
	networkLogger := CreateNetworkLogger(t, config)
	networkLogger.Info("Console only network log")

	// Create node logger - should not panic
	nodeID := testutil.GenerateNodeID(t)
	nodeLogger := CreateNodeLogger(t, config, nodeID)
	nodeLogger.Info("Console only node log")

	// No files should be created (test passes if no panic)
}
