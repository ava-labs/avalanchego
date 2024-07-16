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

func (n *Node) getFlagsPath() string {
	return filepath.Join(n.GetDataDir(), "flags.json")
}

func (n *Node) readFlags() error {
	bytes, err := os.ReadFile(n.getFlagsPath())
	if err != nil {
		return fmt.Errorf("failed to read node flags: %w", err)
	}
	flags := FlagsMap{}
	if err := json.Unmarshal(bytes, &flags); err != nil {
		return fmt.Errorf("failed to unmarshal node flags: %w", err)
	}
	n.Flags = flags
	return n.EnsureNodeID()
}

func (n *Node) writeFlags() error {
	bytes, err := DefaultJSONMarshal(n.Flags)
	if err != nil {
		return fmt.Errorf("failed to marshal node flags: %w", err)
	}
	if err := os.WriteFile(n.getFlagsPath(), bytes, perms.ReadWrite); err != nil {
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
	NetworkUUID   string
	NetworkOwner  string
	IsEphemeral   bool
	RuntimeConfig *NodeRuntimeConfig
}

func (n *Node) writeConfig() error {
	config := serializedNodeConfig{
		NetworkUUID:   n.NetworkUUID,
		NetworkOwner:  n.NetworkOwner,
		IsEphemeral:   n.IsEphemeral,
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
	if err := n.readFlags(); err != nil {
		return err
	}
	if err := n.readConfig(); err != nil {
		return err
	}
	return n.readState()
}

func (n *Node) Write() error {
	if err := os.MkdirAll(n.GetDataDir(), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create node dir: %w", err)
	}

	if err := n.writeFlags(); err != nil {
		return err
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
