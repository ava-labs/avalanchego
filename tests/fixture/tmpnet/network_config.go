// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// The Network type is defined in this file (reading/writing configuration) and network.go
// (orchestration).

var errMissingNetworkDir = errors.New("failed to write network: missing network directory")

// Read network and node configuration from disk.
func (n *Network) Read() error {
	if err := n.readNetwork(); err != nil {
		return err
	}
	if err := n.readNodes(); err != nil {
		return err
	}
	return n.readSubnets()
}

// Write network configuration to disk.
func (n *Network) Write() error {
	if len(n.Dir) == 0 {
		return errMissingNetworkDir
	}
	if err := n.writeGenesis(); err != nil {
		return err
	}
	if err := n.writeNetworkConfig(); err != nil {
		return err
	}
	if err := n.writeEnvFile(); err != nil {
		return err
	}
	return n.writeNodes()
}

// Read network configuration from disk.
func (n *Network) readNetwork() error {
	if err := n.readGenesis(); err != nil {
		return err
	}
	return n.readConfig()
}

// Read the non-ephemeral nodes associated with the network from disk.
func (n *Network) readNodes() error {
	nodes, err := ReadNodes(n.Dir, false /* includeEphemeral */)
	if err != nil {
		return err
	}
	n.Nodes = nodes
	return nil
}

func (n *Network) writeNodes() error {
	for _, node := range n.Nodes {
		if err := node.Write(); err != nil {
			return err
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
		return fmt.Errorf("failed to read genesis: %w", err)
	}
	genesis := genesis.UnparsedConfig{}
	if err := json.Unmarshal(bytes, &genesis); err != nil {
		return fmt.Errorf("failed to unmarshal genesis: %w", err)
	}
	n.Genesis = &genesis
	return nil
}

func (n *Network) writeGenesis() error {
	bytes, err := DefaultJSONMarshal(n.Genesis)
	if err != nil {
		return fmt.Errorf("failed to marshal genesis: %w", err)
	}
	if err := os.WriteFile(n.GetGenesisPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write genesis: %w", err)
	}
	return nil
}

func (n *Network) getConfigPath() string {
	return filepath.Join(n.Dir, defaultConfigFilename)
}

func (n *Network) readConfig() error {
	bytes, err := os.ReadFile(n.getConfigPath())
	if err != nil {
		return fmt.Errorf("failed to read network config: %w", err)
	}
	if err := json.Unmarshal(bytes, n); err != nil {
		return fmt.Errorf("failed to unmarshal network config: %w", err)
	}
	return nil
}

// The subset of network fields to store in the network config file.
type serializedNetworkConfig struct {
	UUID                 string
	Owner                string
	PrimarySubnetConfigs map[ids.ID]subnets.Config
	PrimaryChainConfigs  map[string]FlagsMap
	DefaultFlags         FlagsMap
	DefaultRuntimeConfig NodeRuntimeConfig
	PreFundedKeys        []*secp256k1.PrivateKey
}

func (n *Network) writeNetworkConfig() error {
	config := &serializedNetworkConfig{
		UUID:                 n.UUID,
		Owner:                n.Owner,
		PrimarySubnetConfigs: n.PrimarySubnetConfigs,
		PrimaryChainConfigs:  n.PrimaryChainConfigs,
		DefaultFlags:         n.DefaultFlags,
		DefaultRuntimeConfig: n.DefaultRuntimeConfig,
		PreFundedKeys:        n.PreFundedKeys,
	}
	bytes, err := DefaultJSONMarshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal network config: %w", err)
	}
	if err := os.WriteFile(n.getConfigPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write network config: %w", err)
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
		return fmt.Errorf("failed to write network env file: %w", err)
	}
	return nil
}

func (n *Network) GetSubnetDir() string {
	return filepath.Join(n.Dir, defaultSubnetDirName)
}

func (n *Network) readSubnets() error {
	subnets, err := readSubnets(n.GetSubnetDir())
	if err != nil {
		return err
	}
	n.Subnets = subnets
	return nil
}
