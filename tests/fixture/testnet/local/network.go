// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
)

// This interval was chosen to avoid spamming node APIs during
// startup, as smaller intervals (e.g. 50ms) seemed to noticeably
// increase the time for a network's nodes to be seen as healthy.
const networkHealthCheckInterval = 200 * time.Millisecond

var (
	errInvalidNodeCount        = errors.New("failed to populate local network config: non-zero node count is only valid for a network without nodes")
	errInvalidKeyCount         = errors.New("failed to populate local network config: non-zero key count is only valid for a network without keys")
	errLocalNetworkDirNotSet   = errors.New("local network directory not set - has Create() been called?")
	errInvalidNetworkDir       = errors.New("failed to write local network: invalid network directory")
	errNotHealthyBeforeTimeout = errors.New("failed to see all nodes healthy before timeout")
)

// Default root dir for storing networks and their configuration.
func GetDefaultRootDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".testnetctl", "networks"), nil
}

// Find the next available network ID by attempting to create a
// directory numbered from 1000 until creation succeeds. Returns the
// network id and the full path of the created directory.
func FindNextNetworkID(rootDir string) (uint32, string, error) {
	var (
		networkID uint32 = 1000
		dirPath   string
	)
	for {
		_, reserved := constants.NetworkIDToNetworkName[networkID]
		if reserved {
			continue
		}

		dirPath = filepath.Join(rootDir, fmt.Sprint(networkID))
		err := os.Mkdir(dirPath, perms.ReadWriteExecute)
		if err == nil {
			return networkID, dirPath, nil
		}

		if !errors.Is(err, fs.ErrExist) {
			return 0, "", fmt.Errorf("failed to create network directory: %w", err)
		}

		// Directory already exists, keep iterating
		networkID++
	}
}

// Defines the configuration required for a local network (i.e. one composed of local processes).
type LocalNetwork struct {
	testnet.NetworkConfig
	LocalConfig

	// Nodes with local configuration
	Nodes []*LocalNode

	// Path where network configuration will be stored
	Dir string
}

// Returns the configuration of the network in backend-agnostic form.
func (ln *LocalNetwork) GetConfig() testnet.NetworkConfig {
	return ln.NetworkConfig
}

// Returns the nodes of the network in backend-agnostic form.
func (ln *LocalNetwork) GetNodes() []testnet.Node {
	nodes := make([]testnet.Node, 0, len(ln.Nodes))
	for _, node := range ln.Nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// Starts a new network stored under the provided root dir. Required
// configuration will be defaulted if not provided.
func StartNetwork(
	ctx context.Context,
	w io.Writer,
	rootDir string,
	network *LocalNetwork,
	nodeCount int,
	keyCount int,
) (*LocalNetwork, error) {
	if _, err := fmt.Fprintln(w, "Preparing configuration for new local network..."); err != nil {
		return nil, err
	}

	if len(rootDir) == 0 {
		// Use the default root dir
		var err error
		rootDir, err = GetDefaultRootDir()
		if err != nil {
			return nil, err
		}
	}

	// Ensure creation of the root dir
	if err := os.MkdirAll(rootDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("failed to create root network dir: %w", err)
	}

	// Determine the network path and ID
	var (
		networkDir string
		networkID  uint32
	)
	if network.Genesis != nil && network.Genesis.NetworkID > 0 {
		// Use the network ID defined in the provided genesis
		networkID = network.Genesis.NetworkID
	}
	if networkID > 0 {
		// Use a directory with a random suffix
		var err error
		networkDir, err = os.MkdirTemp(rootDir, fmt.Sprintf("%d.", network.Genesis.NetworkID))
		if err != nil {
			return nil, fmt.Errorf("failed to create network dir: %w", err)
		}
	} else {
		// Find the next available network ID based on the contents of the root dir
		var err error
		networkID, networkDir, err = FindNextNetworkID(rootDir)
		if err != nil {
			return nil, err
		}
	}

	// Setting the network dir before populating config ensures the
	// nodes know where to write their configuration.
	network.Dir = networkDir

	if err := network.PopulateLocalNetworkConfig(networkID, nodeCount, keyCount); err != nil {
		return nil, err
	}

	if err := network.WriteAll(); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "Starting network %d @ %s\n", network.Genesis.NetworkID, network.Dir); err != nil {
		return nil, err
	}
	if err := network.Start(w); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "Waiting for all nodes to report healthy...\n\n"); err != nil {
		return nil, err
	}
	if err := network.WaitForHealthy(ctx, w); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "\nStarted network %d @ %s\n", network.Genesis.NetworkID, network.Dir); err != nil {
		return nil, err
	}
	return network, nil
}

// Read a network from the provided directory.
func ReadNetwork(dir string) (*LocalNetwork, error) {
	network := &LocalNetwork{Dir: dir}
	if err := network.ReadAll(); err != nil {
		return nil, fmt.Errorf("failed to read local network: %w", err)
	}
	return network, nil
}

// Stop the nodes of the network configured in the provided directory.
func StopNetwork(dir string) error {
	network, err := ReadNetwork(dir)
	if err != nil {
		return err
	}
	return network.Stop()
}

// Ensure the network has the configuration it needs to start.
func (ln *LocalNetwork) PopulateLocalNetworkConfig(networkID uint32, nodeCount int, keyCount int) error {
	if len(ln.Nodes) > 0 && nodeCount > 0 {
		return errInvalidNodeCount
	}
	if len(ln.FundedKeys) > 0 && keyCount > 0 {
		return errInvalidKeyCount
	}

	if nodeCount > 0 {
		// Add the specified number of nodes
		nodes := make([]*LocalNode, 0, nodeCount)
		for i := 0; i < nodeCount; i++ {
			nodes = append(nodes, NewLocalNode(""))
		}
		ln.Nodes = nodes
	}

	// Ensure each node has keys and an associated node ID. This
	// ensures the availability of validator node IDs for genesis
	// generation.
	for _, node := range ln.Nodes {
		if err := node.EnsureKeys(); err != nil {
			return err
		}
	}

	// Assume all initial nodes are validator ids
	validatorIDs := make([]ids.NodeID, 0, len(ln.Nodes))
	for _, node := range ln.Nodes {
		validatorIDs = append(validatorIDs, node.NodeID)
	}

	if keyCount > 0 {
		// Ensure there are keys for genesis generation to fund
		factory := secp256k1.Factory{}
		keys := make([]*secp256k1.PrivateKey, 0, keyCount)
		for i := 0; i < keyCount; i++ {
			key, err := factory.NewPrivateKey()
			if err != nil {
				return fmt.Errorf("failed to generate private key: %w", err)
			}
			keys = append(keys, key)
		}
		ln.FundedKeys = keys
	}

	if err := ln.EnsureGenesis(networkID, validatorIDs); err != nil {
		return err
	}

	if ln.CChainConfig == nil {
		ln.CChainConfig = LocalCChainConfig()
	}

	// Default flags need to be set in advance of node config
	// population to ensure correct node configuration.
	if ln.DefaultFlags == nil {
		ln.DefaultFlags = LocalFlags()
	}

	for _, node := range ln.Nodes {
		// Ensure the node is configured for use with the network and
		// knows where to write its configuration.
		if err := ln.PopulateNodeConfig(node); err != nil {
			return err
		}
	}

	return nil
}

// Ensure the provided node has the configuration it needs to
// start. Requires that the network has valid genesis data.
func (ln *LocalNetwork) PopulateNodeConfig(node *LocalNode) error {
	flags := node.Flags

	// Set values common to all nodes
	flags.SetDefaults(ln.DefaultFlags)
	flags.SetDefaults(testnet.FlagsMap{
		config.GenesisFileKey:    ln.GetGenesisPath(),
		config.ChainConfigDirKey: ln.GetChainConfigDir(),
	})

	// Convert the network id to a string to ensure consistency in JSON round-tripping.
	flags[config.NetworkNameKey] = fmt.Sprintf("%d", ln.Genesis.NetworkID)

	// Ensure keys are added if necessary
	if err := node.EnsureKeys(); err != nil {
		return err
	}

	// Ensure the node's data dir is configured
	dataDir := node.GetDataDir()
	if len(dataDir) == 0 {
		// NodeID will have been set by EnsureKeys
		dataDir = filepath.Join(ln.Dir, node.NodeID.String())
		flags[config.DataDirKey] = dataDir
	}

	return nil
}

// Starts a network for the first time
func (ln *LocalNetwork) Start(w io.Writer) error {
	if len(ln.Dir) == 0 {
		return errLocalNetworkDirNotSet
	}

	// Ensure configuration on disk is current
	if err := ln.WriteAll(); err != nil {
		return err
	}

	// Accumulate bootstrap nodes such that each subsequently started
	// node bootstraps from the nodes previously started.
	//
	// e.g.
	// 1st node: no bootstrap nodes
	// 2nd node: 1st node
	// 3rd node: 1st and 2nd nodes
	// ...
	//
	bootstrapIDs := make([]string, 0, len(ln.Nodes))
	bootstrapIPs := make([]string, 0, len(ln.Nodes))

	// Configure networking and start each node
	for _, node := range ln.Nodes {
		// Update network configuration
		node.SetNetworkingConfigDefaults(0, 0, bootstrapIDs, bootstrapIPs)

		// Write configuration to disk in preparation for node start
		if err := node.WriteConfig(); err != nil {
			return err
		}

		// Start waits for the process context to be written which
		// indicates that the node will be accepting connections on
		// its staking port. The network will start faster with this
		// synchronization due to the avoidance of exponential backoff
		// if a node tries to connect to a beacon that is not ready.
		if err := node.Start(w, ln.ExecPath); err != nil {
			return err
		}

		// Collect bootstrap nodes for subsequently started nodes to use
		bootstrapIDs = append(bootstrapIDs, node.NodeID.String())
		bootstrapIPs = append(bootstrapIPs, node.StakingAddress)
	}

	return nil
}

// Wait until all nodes in the network are healthy.
func (ln *LocalNetwork) WaitForHealthy(ctx context.Context, w io.Writer) error {
	ticker := time.NewTicker(networkHealthCheckInterval)
	defer ticker.Stop()

	healthyNodes := set.NewSet[ids.NodeID](len(ln.Nodes))
	for healthyNodes.Len() < len(ln.Nodes) {
		for _, node := range ln.Nodes {
			if healthyNodes.Contains(node.NodeID) {
				continue
			}

			healthy, err := node.IsHealthy(ctx)
			if err != nil && !errors.Is(err, errProcessNotRunning) {
				return err
			}
			if !healthy {
				continue
			}

			healthyNodes.Add(node.NodeID)
			if _, err := fmt.Fprintf(w, "%s is healthy @ %s\n", node.NodeID, node.URI); err != nil {
				return err
			}
		}

		select {
		case <-ctx.Done():
			return errNotHealthyBeforeTimeout
		case <-ticker.C:
		}
	}
	return nil
}

// Retrieve API URIs for all nodes in the network. Assumes nodes have
// been loaded.
func (ln *LocalNetwork) GetURIs() []string {
	uris := make([]string, 0, len(ln.Nodes))
	for _, node := range ln.Nodes {
		// Only append URIs that are not empty. A node may have an
		// empty URI if it was not running at the time
		// node.ReadProcessContext() was called.
		if len(node.URI) > 0 {
			uris = append(uris, node.URI)
		}
	}
	return uris
}

type NodeStopError struct {
	NodeID    ids.NodeID
	StopError error
}

func (e *NodeStopError) Error() string {
	return fmt.Sprintf("failed to stop node %s: %v", e.NodeID, e.StopError)
}

type NetworkStopError struct {
	Errors []*NodeStopError
}

func (e *NetworkStopError) Error() string {
	return fmt.Sprintf("failed to stop network: %v", e.Errors)
}

// Stop all nodes in the network.
func (ln *LocalNetwork) Stop() error {
	errs := []*NodeStopError{}
	// Assume the nodes are loaded and the pids are current
	for _, node := range ln.Nodes {
		if err := node.Stop(); err != nil {
			errs = append(errs, &NodeStopError{
				NodeID:    node.NodeID,
				StopError: err,
			})
		}
	}
	if len(errs) > 0 {
		// TODO(marun) When avalanchego updates to go 1.20, update to
		// use the new capability to wrap multiple errors.
		return &NetworkStopError{Errors: errs}
	}
	return nil
}

func (ln *LocalNetwork) GetGenesisPath() string {
	return filepath.Join(ln.Dir, "genesis.json")
}

func (ln *LocalNetwork) ReadGenesis() error {
	bytes, err := os.ReadFile(ln.GetGenesisPath())
	if err != nil {
		return fmt.Errorf("failed to read genesis: %w", err)
	}
	genesis := genesis.UnparsedConfig{}
	if err := json.Unmarshal(bytes, &genesis); err != nil {
		return fmt.Errorf("failed to unmarshal genesis: %w", err)
	}
	ln.Genesis = &genesis
	return nil
}

func (ln *LocalNetwork) WriteGenesis() error {
	bytes, err := testnet.DefaultJSONMarshal(ln.Genesis)
	if err != nil {
		return fmt.Errorf("failed to marshal genesis: %w", err)
	}
	if err := os.WriteFile(ln.GetGenesisPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write genesis: %w", err)
	}
	return nil
}

func (ln *LocalNetwork) GetChainConfigDir() string {
	return filepath.Join(ln.Dir, "chains")
}

func (ln *LocalNetwork) GetCChainConfigPath() string {
	return filepath.Join(ln.GetChainConfigDir(), "C", "config.json")
}

func (ln *LocalNetwork) ReadCChainConfig() error {
	chainConfig, err := testnet.ReadFlagsMap(ln.GetCChainConfigPath(), "C-Chain config")
	if err != nil {
		return err
	}
	ln.CChainConfig = *chainConfig
	return nil
}

func (ln *LocalNetwork) WriteCChainConfig() error {
	path := ln.GetCChainConfigPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create C-Chain config dir: %w", err)
	}
	return ln.CChainConfig.Write(path, "C-Chain config")
}

// Used to marshal/unmarshal persistent local network defaults.
type localDefaults struct {
	Flags      testnet.FlagsMap
	ExecPath   string
	FundedKeys []*secp256k1.PrivateKey
}

func (ln *LocalNetwork) GetDefaultsPath() string {
	return filepath.Join(ln.Dir, "defaults.json")
}

func (ln *LocalNetwork) ReadDefaults() error {
	bytes, err := os.ReadFile(ln.GetDefaultsPath())
	if err != nil {
		return fmt.Errorf("failed to read defaults: %w", err)
	}
	defaults := localDefaults{}
	if err := json.Unmarshal(bytes, &defaults); err != nil {
		return fmt.Errorf("failed to unmarshal defaults: %w", err)
	}
	ln.DefaultFlags = defaults.Flags
	ln.ExecPath = defaults.ExecPath
	ln.FundedKeys = defaults.FundedKeys
	return nil
}

func (ln *LocalNetwork) WriteDefaults() error {
	defaults := localDefaults{
		Flags:      ln.DefaultFlags,
		ExecPath:   ln.ExecPath,
		FundedKeys: ln.FundedKeys,
	}
	bytes, err := testnet.DefaultJSONMarshal(defaults)
	if err != nil {
		return fmt.Errorf("failed to marshal defaults: %w", err)
	}
	if err := os.WriteFile(ln.GetDefaultsPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write defaults: %w", err)
	}
	return nil
}

func (ln *LocalNetwork) EnvFilePath() string {
	return filepath.Join(ln.Dir, "network.env")
}

func (ln *LocalNetwork) EnvFileContents() string {
	return fmt.Sprintf("export %s=%s", NetworkDirEnvName, ln.Dir)
}

// Write an env file that sets the network dir env when sourced.
func (ln *LocalNetwork) WriteEnvFile() error {
	if err := os.WriteFile(ln.EnvFilePath(), []byte(ln.EnvFileContents()), perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write local network env file: %w", err)
	}
	return nil
}

func (ln *LocalNetwork) WriteNodes() error {
	for _, node := range ln.Nodes {
		if err := node.WriteConfig(); err != nil {
			return err
		}
	}
	return nil
}

// Write network configuration to disk.
func (ln *LocalNetwork) WriteAll() error {
	if len(ln.Dir) == 0 {
		return errInvalidNetworkDir
	}
	if err := ln.WriteGenesis(); err != nil {
		return err
	}
	if err := ln.WriteCChainConfig(); err != nil {
		return err
	}
	if err := ln.WriteDefaults(); err != nil {
		return err
	}
	if err := ln.WriteEnvFile(); err != nil {
		return err
	}
	return ln.WriteNodes()
}

// Read network configuration from disk.
func (ln *LocalNetwork) ReadConfig() error {
	if err := ln.ReadGenesis(); err != nil {
		return err
	}
	if err := ln.ReadCChainConfig(); err != nil {
		return err
	}
	return ln.ReadDefaults()
}

// Read node configuration and process context from disk.
func (ln *LocalNetwork) ReadNodes() error {
	nodes := []*LocalNode{}

	// Node configuration / process context is stored in child directories
	entries, err := os.ReadDir(ln.Dir)
	if err != nil {
		return fmt.Errorf("failed to read network path: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		nodeDir := filepath.Join(ln.Dir, entry.Name())
		node, err := ReadNode(nodeDir)
		if errors.Is(err, os.ErrNotExist) {
			// If no config file exists, assume this is not the path of a local node
			continue
		} else if err != nil {
			return err
		}

		nodes = append(nodes, node)
	}

	ln.Nodes = nodes

	return nil
}

// Read network and node configuration from disk.
func (ln *LocalNetwork) ReadAll() error {
	if err := ln.ReadConfig(); err != nil {
		return err
	}
	return ln.ReadNodes()
}
