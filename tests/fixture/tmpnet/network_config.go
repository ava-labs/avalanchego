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
	if err := n.writeChainConfigs(); err != nil {
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
	if err := n.readChainConfigs(); err != nil {
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

func (n *Network) getGenesisPath() string {
	return filepath.Join(n.Dir, "genesis.json")
}

func (n *Network) readGenesis() error {
	bytes, err := os.ReadFile(n.getGenesisPath())
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
	if err := os.WriteFile(n.getGenesisPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write genesis: %w", err)
	}
	return nil
}

func (n *Network) GetChainConfigDir() string {
	return filepath.Join(n.Dir, "chains")
}

func (n *Network) readChainConfigs() error {
	baseChainConfigDir := n.GetChainConfigDir()
	entries, err := os.ReadDir(baseChainConfigDir)
	if err != nil {
		return fmt.Errorf("failed to read chain config dir: %w", err)
	}

	// Clear the map of data that may end up stale (e.g. if a given
	// chain is in the map but no longer exists on disk)
	n.ChainConfigs = map[string]FlagsMap{}

	for _, entry := range entries {
		if !entry.IsDir() {
			// Chain config files are expected to be nested under a
			// directory with the name of the chain alias.
			continue
		}
		chainAlias := entry.Name()
		configPath := filepath.Join(baseChainConfigDir, chainAlias, defaultConfigFilename)
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			// No config file present
			continue
		}
		chainConfig, err := ReadFlagsMap(configPath, chainAlias+" chain config")
		if err != nil {
			return err
		}
		n.ChainConfigs[chainAlias] = chainConfig
	}

	return nil
}

func (n *Network) writeChainConfigs() error {
	baseChainConfigDir := n.GetChainConfigDir()

	for chainAlias, chainConfig := range n.ChainConfigs {
		// Create the directory
		chainConfigDir := filepath.Join(baseChainConfigDir, chainAlias)
		if err := os.MkdirAll(chainConfigDir, perms.ReadWriteExecute); err != nil {
			return fmt.Errorf("failed to create %s chain config dir: %w", chainAlias, err)
		}

		// Write the file
		path := filepath.Join(chainConfigDir, defaultConfigFilename)
		if err := chainConfig.Write(path, chainAlias+" chain config"); err != nil {
			return err
		}
	}

	// TODO(marun) Ensure the removal of chain aliases that aren't present in the map

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
	DefaultFlags         FlagsMap
	DefaultRuntimeConfig NodeRuntimeConfig
	PreFundedKeys        []*secp256k1.PrivateKey
}

func (n *Network) writeNetworkConfig() error {
	config := &serializedNetworkConfig{
		UUID:                 n.UUID,
		Owner:                n.Owner,
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
