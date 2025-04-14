// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/utils/perms"
)

// The Node type is defined in this file node_config.go
// (reading/writing configuration) and node.go (orchestration).

// For consumption outside of avalanchego. Needs to be kept exported.
func (n *Node) GetFlagsPath() string {
	return filepath.Join(n.GetDataDir(), "flags.json")
}

func (n *Node) writeFlags(flags FlagsMap) error {
	bytes, err := DefaultJSONMarshal(flags)
	if err != nil {
		return fmt.Errorf("failed to marshal node flags: %w", err)
	}
	if err := os.WriteFile(n.GetFlagsPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write node flags: %w", err)
	}
	return nil
}

func (n *Node) getConfigPath() string {
	return filepath.Join(n.GetDataDir(), defaultConfigFilename)
}

func (n *Node) readConfig() error {
	bytes, err := os.ReadFile(n.getConfigPath())
	if err != nil {
		return fmt.Errorf("failed to read node config: %w", err)
	}
	if err := json.Unmarshal(bytes, n); err != nil {
		return fmt.Errorf("failed to unmarshal node config: %w", err)
	}
	return nil
}

type serializedNodeConfig struct {
	IsEphemeral   bool               `json:",omitempty"`
	Flags         FlagsMap           `json:",omitempty"`
	RuntimeConfig *NodeRuntimeConfig `json:",omitempty"`
}

func (n *Node) writeConfig() error {
	config := serializedNodeConfig{
		IsEphemeral:   n.IsEphemeral,
		Flags:         n.Flags,
		RuntimeConfig: n.RuntimeConfig,
	}
	bytes, err := DefaultJSONMarshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal node config: %w", err)
	}
	if err := os.WriteFile(n.getConfigPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write node config: %w", err)
	}
	return nil
}

func (n *Node) Read() error {
	if err := n.readConfig(); err != nil {
		return err
	}
	return n.readState()
}

func (n *Node) Write() error {
	if err := os.MkdirAll(n.GetDataDir(), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create node dir: %w", err)
	}
	return n.writeConfig()
}

func (n *Node) writeMetricsSnapshot(data []byte) error {
	metricsDir := filepath.Join(n.GetDataDir(), "metrics")
	if err := os.MkdirAll(metricsDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create metrics dir: %w", err)
	}
	// Create a compatible filesystem from the current timestamp
	ts := time.Now().UTC().Format(time.RFC3339)
	ts = strings.ReplaceAll(strings.ReplaceAll(ts, ":", ""), "-", "")
	metricsPath := filepath.Join(metricsDir, ts)
	return os.WriteFile(metricsPath, data, perms.ReadWrite)
}
