// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// The Network type is defined in this file (reading/writing configuration) and network.go
// (orchestration).

var errMissingNetworkDir = errors.New("failed to write network: missing network directory")

// Read network and node configuration from disk.
func (n *Network) Read(ctx context.Context) error {
	if err := n.readNetwork(); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := n.readNodes(ctx); err != nil {
		return stacktrace.Wrap(err)
	}
	return n.readSubnets()
}

// Write network configuration to disk.
func (n *Network) Write() error {
	if len(n.Dir) == 0 {
		return stacktrace.Wrap(errMissingNetworkDir)
	}
	if err := n.writeGenesis(); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := n.writeNetworkConfig(); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := n.writeEnvFile(); err != nil {
		return stacktrace.Wrap(err)
	}
	return stacktrace.Wrap(n.writeNodes())
}

// Read network configuration from disk.
func (n *Network) readNetwork() error {
	if err := n.readGenesis(); err != nil {
		return stacktrace.Wrap(err)
	}
	return n.readConfig()
}

// Read the nodes associated with the network from disk.
func (n *Network) readNodes(ctx context.Context) error {
	nodes := []*Node{}

	// Node configuration is stored in child directories
	entries, err := os.ReadDir(n.Dir)
	if err != nil {
		return stacktrace.Errorf("failed to read dir: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		node := NewNode()
		dataDir := filepath.Join(n.Dir, entry.Name())
		err := node.Read(ctx, n, dataDir)
		if errors.Is(err, os.ErrNotExist) {
			// If no config file exists, assume this is not the path of a node
			continue
		} else if err != nil {
			return stacktrace.Wrap(err)
		}

		nodes = append(nodes, node)
	}

	n.Nodes = nodes

	return nil
}

func (n *Network) writeNodes() error {
	for _, node := range n.Nodes {
		if err := node.Write(); err != nil {
			return stacktrace.Wrap(err)
		}
	}
	return nil
}

// For consumption outside of avalanchego. Needs to be kept exported.
func (n *Network) GetGenesisPath() string {
	return filepath.Join(n.Dir, "genesis.json")
}

func (n *Network) readGenesis() error {
	bytes, err := os.ReadFile(n.GetGenesisPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			n.Genesis = nil
			return nil
		}
		return stacktrace.Errorf("failed to read genesis: %w", err)
	}
	genesis := genesis.UnparsedConfig{}
	if err := json.Unmarshal(bytes, &genesis); err != nil {
		return stacktrace.Errorf("failed to unmarshal genesis: %w", err)
	}
	n.Genesis = &genesis
	return nil
}

func (n *Network) writeGenesis() error {
	if n.Genesis == nil {
		return nil
	}
	bytes, err := DefaultJSONMarshal(n.Genesis)
	if err != nil {
		return stacktrace.Errorf("failed to marshal genesis: %w", err)
	}
	if err := os.WriteFile(n.GetGenesisPath(), bytes, perms.ReadWrite); err != nil {
		return stacktrace.Errorf("failed to write genesis: %w", err)
	}
	return nil
}

func (n *Network) getConfigPath() string {
	return filepath.Join(n.Dir, defaultConfigFilename)
}

func (n *Network) readConfig() error {
	bytes, err := os.ReadFile(n.getConfigPath())
	if err != nil {
		return stacktrace.Errorf("failed to read network config: %w", err)
	}
	if err := json.Unmarshal(bytes, n); err != nil {
		return stacktrace.Errorf("failed to unmarshal network config: %w", err)
	}
	return nil
}

// The subset of network fields to store in the network config file.
type serializedNetworkConfig struct {
	UUID                 string                  `json:"uuid,omitempty"`
	Owner                string                  `json:"owner,omitempty"`
	NetworkID            uint32                  `json:"networkID,omitempty"`
	PrimarySubnetConfig  ConfigMap               `json:"primarySubnetConfig,omitempty"`
	PrimaryChainConfigs  map[string]ConfigMap    `json:"primaryChainConfigs,omitempty"`
	DefaultFlags         FlagsMap                `json:"defaultFlags,omitempty"`
	DefaultRuntimeConfig NodeRuntimeConfig       `json:"defaultRuntimeConfig,omitempty"`
	PreFundedKeys        []*secp256k1.PrivateKey `json:"preFundedKeys,omitempty"`
}

func (n *Network) writeNetworkConfig() error {
	config := &serializedNetworkConfig{
		UUID:                 n.UUID,
		Owner:                n.Owner,
		NetworkID:            n.NetworkID,
		PrimarySubnetConfig:  n.PrimarySubnetConfig,
		PrimaryChainConfigs:  n.PrimaryChainConfigs,
		DefaultFlags:         n.DefaultFlags,
		DefaultRuntimeConfig: n.DefaultRuntimeConfig,
		PreFundedKeys:        n.PreFundedKeys,
	}
	bytes, err := DefaultJSONMarshal(config)
	if err != nil {
		return stacktrace.Errorf("failed to marshal network config: %w", err)
	}
	if err := os.WriteFile(n.getConfigPath(), bytes, perms.ReadWrite); err != nil {
		return stacktrace.Errorf("failed to write network config: %w", err)
	}
	return nil
}

func (n *Network) EnvFilePath() string {
	return filepath.Join(n.Dir, "network.env")
}

func (n *Network) EnvFileContents() string {
	return fmt.Sprintf("export %s=%s", NetworkDirEnvName, n.Dir)
}

// Write an env file that sets the network dir env when sourced.
func (n *Network) writeEnvFile() error {
	if err := os.WriteFile(n.EnvFilePath(), []byte(n.EnvFileContents()), perms.ReadWrite); err != nil {
		return stacktrace.Errorf("failed to write network env file: %w", err)
	}
	return nil
}

func (n *Network) GetSubnetDir() string {
	return filepath.Join(n.Dir, defaultSubnetDirName)
}

func (n *Network) readSubnets() error {
	subnets, err := readSubnets(n.GetSubnetDir())
	if err != nil {
		return stacktrace.Wrap(err)
	}
	n.Subnets = subnets
	return nil
}
