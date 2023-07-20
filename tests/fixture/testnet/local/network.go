// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cast"

	cfg "github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
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
	dirPath := ""
	networkID := 1000
	for {
		dirPath = filepath.Join(rootDir, strconv.Itoa(networkID))
		if err := os.Mkdir(dirPath, perms.ReadWriteExecute); err != nil {
			if os.IsExist(err) {
				// Directory already exists, keep iterating
				networkID += 1
				if networkID == int(constants.LocalID) {
					// The local id is reserved
					networkID += 1
				}
				continue
			} else if err != nil {
				return 0, "", err
			}
		}
		break
	}
	return uint32(networkID), dirPath, nil
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
	nodes := []testnet.Node{}
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
		var err error
		rootDir, err = GetDefaultRootDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(rootDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("failed to create root network dir: %w", err)
	}
	networkDir := ""
	networkID := uint32(0)
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
	if err := network.PopulateLocalNetworkConfig(networkID, nodeCount, keyCount); err != nil {
		return nil, err
	}
	network.Dir = networkDir
	if err := network.WriteAll(); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "Starting network %d @ %s\n", network.Genesis.NetworkID, network.Dir); err != nil {
		return nil, err
	}
	if err := network.Start(w); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "Waiting for all nodes to report healthy...\n"); err != nil {
		return nil, err
	}
	if err := network.WaitForHealth(ctx, w); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "Started network %d @ %s\n", network.Genesis.NetworkID, network.Dir); err != nil {
		return nil, err
	}
	return network, nil
}

// Read a network from the provided directory.
func ReadNetwork(dir string) (*LocalNetwork, error) {
	network := &LocalNetwork{Dir: dir}
	if err := network.ReadAll(); err != nil {
		return nil, err
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
		return errors.New("non-zero node count is only valid for a network without nodes")
	}
	if len(ln.FundedKeys) > 0 && keyCount > 0 {
		return errors.New("non-zero key count is only valid for a network without keys")
	}

	if nodeCount > 0 {
		// Add the specified number of nodes
		nodes := []*LocalNode{}
		for i := 0; i < nodeCount; i++ {
			nodes = append(nodes, NewLocalNode(""))
		}
		ln.Nodes = nodes
	}

	// Ensure each node has keys and an associated node ID
	for _, node := range ln.Nodes {
		if err := node.EnsureKeys(); err != nil {
			return err
		}
	}

	// Assume all initial nodes are validator ids
	validatorIDs := []ids.NodeID{}
	for _, node := range ln.Nodes {
		validatorIDs = append(validatorIDs, node.NodeID)
	}

	if keyCount > 0 {
		// Create the specified number of keys
		factory := secp256k1.Factory{}
		keys := []*secp256k1.PrivateKey{}
		for i := 0; i < keyCount; i++ {
			key, err := factory.NewPrivateKey()
			if err != nil {
				return fmt.Errorf("failed to generate private key: %w", err)
			}
			keys = append(keys, key)
		}
		ln.FundedKeys = keys
	}

	if err := ln.PopulateNetworkConfig(networkID, validatorIDs); err != nil {
		return err
	}

	if ln.CChainConfig == nil {
		ln.CChainConfig = LocalCChainConfig()
	}

	if ln.DefaultFlags == nil {
		ln.DefaultFlags = LocalFlags()
	}

	return nil
}

// Ensure the provided node has the configuration it needs to start.
func (ln *LocalNetwork) PopulateNodeConfig(node *LocalNode) error {
	flags := node.Flags

	// Set values common to all nodes
	flags.SetDefaults(ln.DefaultFlags)
	flags.SetDefaults(testnet.FlagsMap{
		cfg.GenesisFileKey:    ln.GetGenesisPath(),
		cfg.ChainConfigDirKey: ln.GetChainConfigDir(),
	})

	// Convert the network id to a string to ensure consistency in JSON round-tripping.
	flags[cfg.NetworkNameKey] = fmt.Sprintf("%d", ln.Genesis.NetworkID)

	// Ensure keys are added if necessary
	if err := node.EnsureKeys(); err != nil {
		return err
	}

	// Ensure the node's data dir is configured
	dataDir := node.GetDataDir()
	if len(dataDir) == 0 {
		// NodeID will have been set by EnsureKeys
		dataDir = filepath.Join(ln.Dir, node.NodeID.String())
		flags[cfg.DataDirKey] = dataDir
	}

	return nil
}

// Starts a network for the first time
func (ln *LocalNetwork) Start(w io.Writer) error {
	if len(ln.Dir) == 0 {
		return errors.New("local network directory not set - has Create() been called?")
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
	bootstrapIDs := []string{}
	bootstrapIPs := []string{}

	// Configure networking and start each node
	for i, node := range ln.Nodes {
		// Update network configuration and write to disk
		httpPort := 0
		stakingPort := 0
		if ln.UseStaticPorts {
			httpPort = int(ln.InitialStaticPort) + i*2
			stakingPort = httpPort + 1
		}
		node.SetNetworkingConfig(httpPort, stakingPort, bootstrapIDs, bootstrapIPs)

		// Write configuration to disk in preparation for node start
		if err := node.WriteConfig(); err != nil {
			return err
		}

		if err := node.Start(w, ln.ExecPath); err != nil {
			return err
		}

		bootstrapIP := ""
		if ln.UseStaticPorts {
			// Infer the bootstrap IP from flag configuration
			rawStakingHost, ok := node.Flags[cfg.StakingHostKey]
			// Only use the default value if the key is not set to allow
			// an empty value (i.e. bind all interfaces) to be supplied.
			stakingHost := "127.0.0.1"
			if ok {
				stakingHost = cast.ToString(rawStakingHost)
			}
			stakingPort := strconv.Itoa(cast.ToInt(node.Flags[cfg.StakingPortKey]))
			bootstrapIP = net.JoinHostPort(stakingHost, stakingPort)
		} else {
			// Use the bootstrap ip from process context
			if err := node.WaitForProcessContext(context.TODO()); err != nil {
				return err
			}
			bootstrapIP = node.StakingAddress
		}
		bootstrapIDs = append(bootstrapIDs, node.NodeID.String())
		bootstrapIPs = append(bootstrapIPs, bootstrapIP)
	}

	return nil
}

// Wait until all nodes in the network are healthy.
func (ln *LocalNetwork) WaitForHealth(ctx context.Context, w io.Writer) error {
	healthyNodes := set.NewSet[ids.NodeID](len(ln.Nodes))
	for {
		for _, node := range ln.Nodes {
			select {
			case <-ctx.Done():
				return errors.New("failed to see healthy nodes before timeout")
			default:
				if healthyNodes.Contains(node.NodeID) {
					continue
				}
				if healthy, err := node.IsHealthy(ctx); err != nil {
					return err
				} else if healthy {
					healthyNodes.Add(node.NodeID)
					if _, err := fmt.Fprintf(w, "%s is healthy @ %s\n", node.NodeID, node.URI); err != nil {
						return err
					}
				}
			}
		}
		if len(healthyNodes) == len(ln.Nodes) {
			break
		}
		time.Sleep(time.Millisecond * 200)
	}
	return nil
}

// Retrieve API URIs for all nodes in the network.
func (ln *LocalNetwork) GetURIs() []string {
	uris := []string{}
	for _, node := range ln.Nodes {
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
	return filepath.Join(ln.GetChainConfigDir(), "C", "cchain_config.json")
}

func (ln *LocalNetwork) ReadCChainConfig() error {
	bytes, err := os.ReadFile(ln.GetCChainConfigPath())
	if err != nil {
		return fmt.Errorf("failed to read cchain config: %w", err)
	}
	cChainConfig := testnet.FlagsMap{}
	if err := json.Unmarshal(bytes, &cChainConfig); err != nil {
		return fmt.Errorf("failed to unmarshal cchain config: %w", err)
	}
	ln.CChainConfig = cChainConfig
	return nil
}

func (ln *LocalNetwork) WriteCChainConfig() error {
	path := ln.GetCChainConfigPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create cchain config dir: %w", err)
	}
	bytes, err := testnet.DefaultJSONMarshal(ln.CChainConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal cchain config: %w", err)
	}
	if err := os.WriteFile(path, bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write cchain config: %w", err)
	}
	return nil
}

// Used to marshal/unmarshal persistent local network defaults.
type localDefaults struct {
	Flags             testnet.FlagsMap
	ExecPath          string
	UseStaticPorts    bool
	InitialStaticPort uint16
	FundedKeys        []*secp256k1.PrivateKey
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
	ln.UseStaticPorts = defaults.UseStaticPorts
	ln.InitialStaticPort = defaults.InitialStaticPort
	ln.FundedKeys = defaults.FundedKeys
	return nil
}

func (ln *LocalNetwork) WriteDefaults() error {
	defaults := localDefaults{
		Flags:          ln.DefaultFlags,
		ExecPath:       ln.ExecPath,
		UseStaticPorts: ln.UseStaticPorts,
		FundedKeys:     ln.FundedKeys,
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
		// Ensure the node is configured for use with the network and knows where to write its
		// configuration.
		if err := ln.PopulateNodeConfig(node); err != nil {
			return err
		}
		if err := node.WriteConfig(); err != nil {
			return err
		}
	}
	return nil
}

// Write network configuration to disk.
func (ln *LocalNetwork) WriteAll() error {
	if len(ln.Dir) == 0 {
		return errors.New("unable to write local network without network directory")
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
	if err := ln.WriteNodes(); err != nil {
		return err
	}
	return nil
}

// Read network configuration from disk.
func (ln *LocalNetwork) ReadConfig() error {
	if err := ln.ReadGenesis(); err != nil {
		return err
	}
	if err := ln.ReadCChainConfig(); err != nil {
		return err
	}
	if err := ln.ReadDefaults(); err != nil {
		return err
	}
	return nil
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
