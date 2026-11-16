// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"go.uber.org/zap/zapcore"
)

// CreateNetworkLogger creates a logger for the network that writes to both console and main.log
func CreateNetworkLogger(t *testing.T, config *FuzzConfig) *testutil.TestLogger {
	if config.LogDirectory == "" {
		return testutil.MakeLogger(t, 0)
	}

	// Clear the log directory before creating new logs
	if err := clearLogDirectory(config.LogDirectory); err != nil {
		t.Fatalf("Failed to clear log directory: %v", err)
	}

	// Create file writer for main.log
	fileWriter, err := setupFileOutput(t, config.LogDirectory, "main.log")
	if err != nil {
		t.Fatalf("Failed to setup file output for main.log: %v", err)
	}

	return testutil.MakeLoggerWithFile(t, fileWriter, true)
}

// CreateNodeLogger creates a logger for a node that writes to both console and {nodeID}.log
func CreateNodeLogger(t *testing.T, config *FuzzConfig, nodeID common.NodeID) *testutil.TestLogger {
	if config.LogDirectory == "" {
		return testutil.MakeLogger(t, int(nodeID[0]))
	}

	filename := fmt.Sprintf("%s.log", nodeID.String())

	// Create file writer for node-specific log
	fileWriter, err := setupFileOutput(t, config.LogDirectory, filename)
	if err != nil {
		t.Fatalf("Failed to setup file output for %s: %v", filename, err)
	}

	return testutil.MakeLoggerWithFile(t, fileWriter, false, int(nodeID[0]))
}

// setupFileOutput creates a file for logging and returns a WriteSyncer.
// The file is closed automatically when the test ends.
func setupFileOutput(t *testing.T, logDir, filename string) (zapcore.WriteSyncer, error) {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory %s: %w", logDir, err)
	}

	// Create full path
	logPath := filepath.Join(logDir, filename)

	// Open file for appending (create if doesn't exist)
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}

	t.Cleanup(func() { _ = file.Close() })

	// Wrap with AddSync to make it safe for concurrent writes
	return zapcore.AddSync(file), nil
}

// clearLogDirectory removes the contents of the log directory
func clearLogDirectory(logDir string) error {
	if err := os.RemoveAll(logDir); err != nil {
		return fmt.Errorf("failed to remove log directory %s: %w", logDir, err)
	}
	return nil
}
