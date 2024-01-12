// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
)

// The Network type is defined in this file (orchestration) and
// network_config.go (reading/writing configuration).

const (
	// Constants defining the names of shell variables whose value can
	// configure network orchestration.
	NetworkDirEnvName = "TMPNET_NETWORK_DIR"
	RootDirEnvName    = "TMPNET_ROOT_DIR"

	// This interval was chosen to avoid spamming node APIs during
	// startup, as smaller intervals (e.g. 50ms) seemed to noticeably
	// increase the time for a network's nodes to be seen as healthy.
	networkHealthCheckInterval = 200 * time.Millisecond
)

// Collects the configuration for running a temporary avalanchego network
type Network struct {
	// Path where network configuration and data is stored
	Dir string

	// Configuration common across nodes
	Genesis      *genesis.UnparsedConfig
	ChainConfigs map[string]FlagsMap

	// Default configuration to use when creating new nodes
	DefaultFlags         FlagsMap
	DefaultRuntimeConfig NodeRuntimeConfig

	// Keys pre-funded in the genesis on both the X-Chain and the C-Chain
	PreFundedKeys []*secp256k1.PrivateKey

	// Nodes that constitute the network
	Nodes []*Node
}

// Ensure a real and absolute network dir so that node
// configuration that embeds the network path will continue to
// work regardless of symlink and working directory changes.
func toCanonicalDir(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absDir)
}

// Initializes a new network with default configuration.
func NewDefaultNetwork(w io.Writer, avalancheGoPath string, nodeCount int) (*Network, error) {
	if _, err := fmt.Fprintf(w, "Preparing configuration for new network with %s\n", avalancheGoPath); err != nil {
		return nil, err
	}

	keys, err := NewPrivateKeys(DefaultPreFundedKeyCount)
	if err != nil {
		return nil, err
	}

	network := &Network{
		DefaultFlags: DefaultFlags(),
		DefaultRuntimeConfig: NodeRuntimeConfig{
			AvalancheGoPath: avalancheGoPath,
		},
		PreFundedKeys: keys,
		ChainConfigs:  DefaultChainConfigs(),
	}

	network.Nodes = make([]*Node, nodeCount)
	for i := range network.Nodes {
		network.Nodes[i] = NewNode("")
		if err := network.EnsureNodeConfig(network.Nodes[i]); err != nil {
			return nil, err
		}
	}

	return network, nil
}

// Stops the nodes of the network configured in the provided directory.
func StopNetwork(ctx context.Context, dir string) error {
	network, err := ReadNetwork(dir)
	if err != nil {
		return err
	}
	return network.Stop(ctx)
}

// Reads a network from the provided directory.
func ReadNetwork(dir string) (*Network, error) {
	canonicalDir, err := toCanonicalDir(dir)
	if err != nil {
		return nil, err
	}
	network := &Network{
		Dir: canonicalDir,
	}
	if err := network.Read(); err != nil {
		return nil, fmt.Errorf("failed to read network: %w", err)
	}
	return network, nil
}

// Creates the network on disk, choosing its network id and generating its genesis in the process.
func (n *Network) Create(rootDir string) error {
	if len(rootDir) == 0 {
		// Use the default root dir
		var err error
		rootDir, err = getDefaultRootDir()
		if err != nil {
			return err
		}
	}

	// Ensure creation of the root dir
	if err := os.MkdirAll(rootDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create root network dir: %w", err)
	}

	// Determine the network path and ID
	var (
		networkDir string
		networkID  uint32
	)
	if n.Genesis != nil && n.Genesis.NetworkID > 0 {
		// Use the network ID defined in the provided genesis
		networkID = n.Genesis.NetworkID
	}
	if networkID > 0 {
		// Use a directory with a random suffix
		var err error
		networkDir, err = os.MkdirTemp(rootDir, fmt.Sprintf("%d.", n.Genesis.NetworkID))
		if err != nil {
			return fmt.Errorf("failed to create network dir: %w", err)
		}
	} else {
		// Find the next available network ID based on the contents of the root dir
		var err error
		networkID, networkDir, err = findNextNetworkID(rootDir)
		if err != nil {
			return err
		}
	}
	canonicalDir, err := toCanonicalDir(networkDir)
	if err != nil {
		return err
	}
	n.Dir = canonicalDir

	if n.Genesis == nil {
		genesis, err := NewTestGenesis(networkID, n.Nodes, n.PreFundedKeys)
		if err != nil {
			return err
		}
		n.Genesis = genesis
	}

	for _, node := range n.Nodes {
		// Ensure the node is configured for use with the network and
		// knows where to write its configuration.
		if err := n.EnsureNodeConfig(node); err != nil {
			return nil
		}
	}

	// Ensure configuration on disk is current
	return n.Write()
}

// Starts all nodes in the network
func (n *Network) Start(ctx context.Context, w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Starting network %d @ %s\n", n.Genesis.NetworkID, n.Dir); err != nil {
		return err
	}

	// Configure the networking for each node and start
	for _, node := range n.Nodes {
		if err := n.StartNode(ctx, w, node); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "Waiting for all nodes to report healthy...\n\n"); err != nil {
		return err
	}
	if err := n.WaitForHealthy(ctx, w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\nStarted network %d @ %s\n", n.Genesis.NetworkID, n.Dir); err != nil {
		return err
	}

	return nil
}

func (n *Network) AddEphemeralNode(ctx context.Context, w io.Writer, flags FlagsMap) (*Node, error) {
	node := NewNode("")
	node.Flags = flags
	node.IsEphemeral = true
	if err := n.StartNode(ctx, w, node); err != nil {
		return nil, err
	}
	return node, nil
}

// Starts the provided node after configuring it for the network.
func (n *Network) StartNode(ctx context.Context, w io.Writer, node *Node) error {
	if err := n.EnsureNodeConfig(node); err != nil {
		return err
	}

	bootstrapIPs, bootstrapIDs, err := n.getBootstrapIPsAndIDs(node)
	if err != nil {
		return err
	}
	node.SetNetworkingConfig(bootstrapIDs, bootstrapIPs)

	if err := node.Write(); err != nil {
		return err
	}

	if err := node.Start(w); err != nil {
		// Attempt to stop an unhealthy node to provide some assurance to the caller
		// that an error condition will not result in a lingering process.
		err = errors.Join(err, node.Stop(ctx))
		return err
	}

	return nil
}

// Waits until all nodes in the network are healthy.
func (n *Network) WaitForHealthy(ctx context.Context, w io.Writer) error {
	ticker := time.NewTicker(networkHealthCheckInterval)
	defer ticker.Stop()

	healthyNodes := set.NewSet[ids.NodeID](len(n.Nodes))
	for healthyNodes.Len() < len(n.Nodes) {
		for _, node := range n.Nodes {
			if healthyNodes.Contains(node.NodeID) {
				continue
			}

			healthy, err := node.IsHealthy(ctx)
			if err != nil && !errors.Is(err, ErrNotRunning) {
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
			return fmt.Errorf("failed to see all nodes healthy before timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

// Stops all nodes in the network.
func (n *Network) Stop(ctx context.Context) error {
	// Target all nodes, including the ephemeral ones
	nodes, err := ReadNodes(n.Dir, true /* includeEphemeral */)
	if err != nil {
		return err
	}

	var errs []error

	// Initiate stop on all nodes
	for _, node := range nodes {
		if err := node.InitiateStop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop node %s: %w", node.NodeID, err))
		}
	}

	// Wait for stop to complete on all nodes
	for _, node := range nodes {
		if err := node.WaitForStopped(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to wait for node %s to stop: %w", node.NodeID, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to stop network:\n%w", errors.Join(errs...))
	}
	return nil
}

// Ensures the provided node has the configuration it needs to start. If the data dir is not
// set, it will be defaulted to [nodeParentDir]/[node ID]. For a not-yet-created network,
// no action will be taken.
// TODO(marun) Reword or refactor to account for the differing behavior pre- vs post-start
func (n *Network) EnsureNodeConfig(node *Node) error {
	flags := node.Flags

	// Set the network name if available
	if n.Genesis != nil && n.Genesis.NetworkID > 0 {
		// Convert the network id to a string to ensure consistency in JSON round-tripping.
		flags[config.NetworkNameKey] = strconv.FormatUint(uint64(n.Genesis.NetworkID), 10)
	}

	if err := node.EnsureKeys(); err != nil {
		return err
	}

	flags.SetDefaults(n.DefaultFlags)

	// Set fields including the network path
	if len(n.Dir) > 0 {
		node.Flags.SetDefaults(FlagsMap{
			config.GenesisFileKey:    n.getGenesisPath(),
			config.ChainConfigDirKey: n.getChainConfigDir(),
		})

		// Ensure the node's data dir is configured
		dataDir := node.getDataDir()
		if len(dataDir) == 0 {
			// NodeID will have been set by EnsureKeys
			dataDir = filepath.Join(n.Dir, node.NodeID.String())
			flags[config.DataDirKey] = dataDir
		}
	}

	// Ensure the node runtime is configured
	if node.RuntimeConfig == nil {
		node.RuntimeConfig = &NodeRuntimeConfig{
			AvalancheGoPath: n.DefaultRuntimeConfig.AvalancheGoPath,
		}
	}

	return nil
}

func (n *Network) GetNodeURIs() []NodeURI {
	return GetNodeURIs(n.Nodes)
}

// Retrieves bootstrap IPs and IDs for all nodes except the skipped one (this supports
// collecting the bootstrap details for restarting a node).
func (n *Network) getBootstrapIPsAndIDs(skippedNode *Node) ([]string, []string, error) {
	// Collect staking addresses of non-ephemeral nodes for use in bootstrapping a node
	nodes, err := ReadNodes(n.Dir, false /* includeEphemeral */)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read network's nodes: %w", err)
	}
	var (
		bootstrapIPs = make([]string, 0, len(nodes))
		bootstrapIDs = make([]string, 0, len(nodes))
	)
	for _, node := range nodes {
		if skippedNode != nil && node.NodeID == skippedNode.NodeID {
			continue
		}

		if len(node.StakingAddress) == 0 {
			// Node is not running
			continue
		}

		bootstrapIPs = append(bootstrapIPs, node.StakingAddress)
		bootstrapIDs = append(bootstrapIDs, node.NodeID.String())
	}

	return bootstrapIPs, bootstrapIDs, nil
}

// Retrieves the default root dir for storing networks and their
// configuration.
func getDefaultRootDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".tmpnet", "networks"), nil
}

// Finds the next available network ID by attempting to create a
// directory numbered from 1000 until creation succeeds. Returns the
// network id and the full path of the created directory.
func findNextNetworkID(rootDir string) (uint32, string, error) {
	var (
		networkID uint32 = 1000
		dirPath   string
	)
	for {
		_, reserved := constants.NetworkIDToNetworkName[networkID]
		if reserved {
			networkID++
			continue
		}

		dirPath = filepath.Join(rootDir, strconv.FormatUint(uint64(networkID), 10))
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
