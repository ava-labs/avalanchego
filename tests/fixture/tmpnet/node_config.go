// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
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
	return filepath.Join(n.DataDir, "flags.json")
}

func (n *Node) getConfigPath() string {
	return filepath.Join(n.DataDir, defaultConfigFilename)
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
	IsEphemeral   bool               `json:"isEphemeral,omitempty"`
	Flags         FlagsMap           `json:"flags,omitempty"`
	RuntimeConfig *NodeRuntimeConfig `json:"runtimeConfig,omitempty"`
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

func (n *Node) Read(ctx context.Context, network *Network, dataDir string) error {
	n.network = network
	n.DataDir = dataDir

	if err := n.readConfig(); err != nil {
		return err
	}
	if err := n.EnsureNodeID(); err != nil {
		return err
	}
	return n.readState(ctx)
}

func (n *Node) Write() error {
	if err := os.MkdirAll(n.DataDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create node dir: %w", err)
	}
	return n.writeConfig()
}

func (n *Node) writeMetricsSnapshot(data []byte) error {
	metricsDir := filepath.Join(n.DataDir, "metrics")
	if err := os.MkdirAll(metricsDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create metrics dir: %w", err)
	}
	// Create a compatible filesystem from the current timestamp
	ts := time.Now().UTC().Format(time.RFC3339)
	ts = strings.ReplaceAll(strings.ReplaceAll(ts, ":", ""), "-", "")
	metricsPath := filepath.Join(metricsDir, ts)
	return os.WriteFile(metricsPath, data, perms.ReadWrite)
}
