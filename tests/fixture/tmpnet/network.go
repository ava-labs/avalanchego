// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
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

	// All temporary networks will use this arbitrary network ID by default.
	defaultNetworkID = 88888

	// eth address: 0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC
	HardHatKeyStr = "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
)

// HardhatKey is a legacy used for hardhat testing in subnet-evm
// TODO(marun) Remove when no longer needed.
var HardhatKey *secp256k1.PrivateKey

func init() {
	hardhatKeyBytes, err := hex.DecodeString(HardHatKeyStr)
	if err != nil {
		panic(err)
	}
	HardhatKey, err = secp256k1.ToPrivateKey(hardhatKeyBytes)
	if err != nil {
		panic(err)
	}
}

// Collects the configuration for running a temporary avalanchego network
type Network struct {
	// Uniquely identifies the temporary network for metrics
	// collection. Distinct from avalanchego's concept of network ID
	// since the utility of special network ID values (e.g. to trigger
	// specific fork behavior in a given network) precludes requiring
	// unique network ID values across all temporary networks.
	UUID string

	// A string identifying the entity that started or maintains this
	// network. Useful for differentiating between networks when a
	// given CI job uses multiple networks.
	Owner string

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

	// Subnets that have been enabled on the network
	Subnets []*Subnet
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

func StartNewNetwork(
	ctx context.Context,
	w io.Writer,
	network *Network,
	rootNetworkDir string,
	avalancheGoExecPath string,
	pluginDir string,
	nodeCount int,
) error {
	if err := network.EnsureDefaultConfig(w, avalancheGoExecPath, pluginDir, nodeCount); err != nil {
		return err
	}
	if err := network.Create(rootNetworkDir); err != nil {
		return err
	}
	return network.Start(ctx, w)
}

// Stops the nodes of the network configured in the provided directory.
func StopNetwork(ctx context.Context, dir string) error {
	network, err := ReadNetwork(dir)
	if err != nil {
		return err
	}
	return network.Stop(ctx)
}

// Restarts the nodes of the network configured in the provided directory.
func RestartNetwork(ctx context.Context, w io.Writer, dir string) error {
	network, err := ReadNetwork(dir)
	if err != nil {
		return err
	}
	return network.Restart(ctx, w)
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

// Initializes a new network with default configuration.
func (n *Network) EnsureDefaultConfig(w io.Writer, avalancheGoPath string, pluginDir string, nodeCount int) error {
	if _, err := fmt.Fprintf(w, "Preparing configuration for new network with %s\n", avalancheGoPath); err != nil {
		return err
	}

	// A UUID supports centralized metrics collection
	if len(n.UUID) == 0 {
		n.UUID = uuid.NewString()
	}

	// Ensure default flags
	if n.DefaultFlags == nil {
		n.DefaultFlags = FlagsMap{}
	}
	n.DefaultFlags.SetDefaults(DefaultFlags())

	// Only configure the plugin dir with a non-empty value to ensure
	// the use of the default value (`[datadir]/plugins`) when
	// no plugin dir is configured.
	if len(pluginDir) > 0 {
		if _, ok := n.DefaultFlags[config.PluginDirKey]; !ok {
			n.DefaultFlags[config.PluginDirKey] = pluginDir
		}
	}

	// Ensure pre-funded keys
	if len(n.PreFundedKeys) == 0 {
		keys, err := NewPrivateKeys(DefaultPreFundedKeyCount)
		if err != nil {
			return err
		}
		n.PreFundedKeys = keys
	}

	// Ensure primary chains are configured
	if n.ChainConfigs == nil {
		n.ChainConfigs = map[string]FlagsMap{}
	}
	defaultChainConfigs := DefaultChainConfigs()
	for alias, chainConfig := range defaultChainConfigs {
		if _, ok := n.ChainConfigs[alias]; !ok {
			n.ChainConfigs[alias] = FlagsMap{}
		}
		n.ChainConfigs[alias].SetDefaults(chainConfig)
	}

	// Ensure runtime is configured
	if len(n.DefaultRuntimeConfig.AvalancheGoPath) == 0 {
		n.DefaultRuntimeConfig.AvalancheGoPath = avalancheGoPath
	}

	// Ensure nodes are created
	if len(n.Nodes) == 0 {
		n.Nodes = NewNodes(nodeCount)
	}

	// Ensure nodes are configured
	for i := range n.Nodes {
		if err := n.EnsureNodeConfig(n.Nodes[i]); err != nil {
			return err
		}
	}

	return nil
}

// Creates the network on disk, generating its genesis and configuring its nodes in the process.
func (n *Network) Create(rootDir string) error {
	// Ensure creation of the root dir
	if len(rootDir) == 0 {
		// Use the default root dir
		var err error
		rootDir, err = getDefaultRootNetworkDir()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(rootDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create root network dir: %w", err)
	}

	// A time-based name ensures consistent directory ordering
	dirName := time.Now().Format("20060102-150405.999999")
	if len(n.Owner) > 0 {
		// Include the owner to differentiate networks created at similar times
		dirName = fmt.Sprintf("%s-%s", dirName, n.Owner)
	}

	// Ensure creation of the network dir
	networkDir := filepath.Join(rootDir, dirName)
	if err := os.MkdirAll(networkDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create network dir: %w", err)
	}
	canonicalDir, err := toCanonicalDir(networkDir)
	if err != nil {
		return err
	}
	n.Dir = canonicalDir

	// Ensure the existence of the plugin directory or nodes won't be able to start.
	pluginDir, err := n.DefaultFlags.GetStringVal(config.PluginDirKey)
	if err != nil {
		return err
	}
	if len(pluginDir) > 0 {
		if err := os.MkdirAll(pluginDir, perms.ReadWriteExecute); err != nil {
			return fmt.Errorf("failed to create plugin dir: %w", err)
		}
	}

	if n.Genesis == nil {
		// Pre-fund known legacy keys to support ad-hoc testing. Usage of a legacy key will
		// require knowing the key beforehand rather than retrieving it from the set of pre-funded
		// keys exposed by a network. Since allocation will not be exclusive, a test using a
		// legacy key is unlikely to be a good candidate for parallel execution.
		keysToFund := []*secp256k1.PrivateKey{
			genesis.VMRQKey,
			genesis.EWOQKey,
			HardhatKey,
		}
		keysToFund = append(keysToFund, n.PreFundedKeys...)

		genesis, err := NewTestGenesis(defaultNetworkID, n.Nodes, keysToFund)
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
	if _, err := fmt.Fprintf(w, "Starting network %s (UUID: %s)\n", n.Dir, n.UUID); err != nil {
		return err
	}

	// Record the time before nodes are started to ensure visibility of subsequently collected metrics via the emitted link
	startTime := time.Now()

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
	if _, err := fmt.Fprintf(w, "\nStarted network %s (UUID: %s)\n", n.Dir, n.UUID); err != nil {
		return err
	}
	// Provide a link to the main dashboard filtered by the uuid and showing results from now till whenever the link is viewed
	if _, err := fmt.Fprintf(w, "\nMetrics: https://grafana-experimental.avax-dev.network/d/kBQpRdWnk/avalanche-main-dashboard?&var-filter=network_uuid%%7C%%3D%%7C%s&var-filter=is_ephemeral_node%%7C%%3D%%7Cfalse&from=%d&to=now\n", n.UUID, startTime.UnixMilli()); err != nil {
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

// Restarts all non-ephemeral nodes in the network.
func (n *Network) Restart(ctx context.Context, w io.Writer) error {
	if _, err := fmt.Fprintf(w, " restarting network\n"); err != nil {
		return err
	}
	for _, node := range n.Nodes {
		// Ensure the node reuses the same API port across restarts to ensure
		// consistent labeling of metrics. Otherwise prometheus's automatic
		// addition of the `instance` label (host:port) results in
		// segmentation of results for a given node every time the port
		// changes on restart. This segmentation causes graphs on the grafana
		// dashboards to display multiple series per graph for a given node,
		// one for each port that the node used.
		//
		// There is a non-zero chance of the port being allocatted to a
		// different process and the node subsequently being unable to start,
		// but the alternative is having to update the grafana dashboards
		// query-by-query to ensure that node metrics ignore the instance
		// label.
		if err := node.SaveAPIPort(); err != nil {
			return err
		}

		if err := node.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop node %s: %w", node.NodeID, err)
		}
		if err := n.StartNode(ctx, w, node); err != nil {
			return fmt.Errorf("failed to start node %s: %w", node.NodeID, err)
		}
		if _, err := fmt.Fprintf(w, " waiting for node %s to report healthy\n", node.NodeID); err != nil {
			return err
		}
		if err := WaitForHealthy(ctx, node); err != nil {
			return err
		}
	}
	return nil
}

// Ensures the provided node has the configuration it needs to start. If the data dir is not
// set, it will be defaulted to [nodeParentDir]/[node ID]. For a not-yet-created network,
// no action will be taken.
// TODO(marun) Reword or refactor to account for the differing behavior pre- vs post-start
func (n *Network) EnsureNodeConfig(node *Node) error {
	flags := node.Flags

	// Ensure nodes can label their metrics with the network uuid
	node.NetworkUUID = n.UUID

	// Ensure nodes can label metrics with an indication of the shared/private nature of the network
	node.NetworkOwner = n.Owner

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

	// Ensure available subnets are tracked
	subnetIDs := make([]string, 0, len(n.Subnets))
	for _, subnet := range n.Subnets {
		if subnet.SubnetID == ids.Empty {
			continue
		}
		subnetIDs = append(subnetIDs, subnet.SubnetID.String())
	}
	flags[config.TrackSubnetsKey] = strings.Join(subnetIDs, ",")

	return nil
}

func (n *Network) GetSubnet(name string) *Subnet {
	for _, subnet := range n.Subnets {
		if subnet.Name == name {
			return subnet
		}
	}
	return nil
}

// Ensure that each subnet on the network is created and that it is validated by all non-ephemeral nodes.
func (n *Network) CreateSubnets(ctx context.Context, w io.Writer) error {
	createdSubnets := make([]*Subnet, 0, len(n.Subnets))
	for _, subnet := range n.Subnets {
		if _, err := fmt.Fprintf(w, "Creating subnet %q\n", subnet.Name); err != nil {
			return err
		}
		if subnet.SubnetID != ids.Empty {
			// The subnet already exists
			continue
		}

		if subnet.OwningKey == nil {
			// Allocate a pre-funded key and remove it from the network so it won't be used for
			// other purposes
			if len(n.PreFundedKeys) == 0 {
				return fmt.Errorf("no pre-funded keys available to create subnet %q", subnet.Name)
			}
			subnet.OwningKey = n.PreFundedKeys[len(n.PreFundedKeys)-1]
			n.PreFundedKeys = n.PreFundedKeys[:len(n.PreFundedKeys)-1]
		}

		// Create the subnet on the network
		if err := subnet.Create(ctx, n.Nodes[0].URI); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, " created subnet %q as %q\n", subnet.Name, subnet.SubnetID); err != nil {
			return err
		}

		// Persist the subnet configuration
		if err := subnet.Write(n.getSubnetDir(), n.getChainConfigDir()); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, " wrote configuration for subnet %q\n", subnet.Name); err != nil {
			return err
		}

		createdSubnets = append(createdSubnets, subnet)
	}

	if len(createdSubnets) == 0 {
		return nil
	}

	// Ensure the in-memory subnet state
	n.Subnets = append(n.Subnets, createdSubnets...)

	// Ensure the pre-funded key changes are persisted to disk
	if err := n.Write(); err != nil {
		return err
	}

	// Reconfigure nodes for the new subnets
	if _, err := fmt.Fprintf(w, "Configured nodes to track new subnet(s). Restart is required.\n"); err != nil {
		return err
	}
	for _, node := range n.Nodes {
		if err := n.EnsureNodeConfig(node); err != nil {
			return err
		}
	}
	// Restart nodes to allow new configuration to take effect
	// TODO(marun) Only restart the validator nodes of newly-created subnets
	if err := n.Restart(ctx, w); err != nil {
		return err
	}

	// Add each node as a subnet validator
	for _, subnet := range createdSubnets {
		if _, err := fmt.Fprintf(w, "Adding validators for subnet %q\n", subnet.Name); err != nil {
			return err
		}
		if err := subnet.AddValidators(ctx, w, n.Nodes); err != nil {
			return err
		}
	}

	// Wait for nodes to become subnet validators
	pChainClient := platformvm.NewClient(n.Nodes[0].URI)
	restartRequired := false
	for _, subnet := range createdSubnets {
		if err := waitForActiveValidators(ctx, w, pChainClient, subnet); err != nil {
			return err
		}

		// It should now be safe to create chains for the subnet
		if err := subnet.CreateChains(ctx, w, n.Nodes[0].URI); err != nil {
			return err
		}

		// Persist the chain configuration
		if err := subnet.Write(n.getSubnetDir(), n.getChainConfigDir()); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, " wrote chain configuration for subnet %q\n", subnet.Name); err != nil {
			return err
		}

		// If one or more of the subnets chains have explicit configuration, the
		// subnet's validator nodes will need to be restarted for those nodes to read
		// the newly written chain configuration and apply it to the chain(s).
		if subnet.HasChainConfig() {
			restartRequired = true
		}
	}

	if !restartRequired {
		return nil
	}

	// Restart nodes to allow configuration for the new chains to take effect
	// TODO(marun) Only restart the validator nodes of subnets that have chains that need configuring
	return n.Restart(ctx, w)
}

func (n *Network) GetURIForNodeID(nodeID ids.NodeID) (string, error) {
	for _, node := range n.Nodes {
		if node.NodeID == nodeID {
			return node.URI, nil
		}
	}
	return "", fmt.Errorf("%s is not known to the network", nodeID)
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

// Retrieves the root dir for tmpnet data.
func getTmpnetPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".tmpnet"), nil
}

// Retrieves the default root dir for storing networks and their
// configuration.
func getDefaultRootNetworkDir() (string, error) {
	tmpnetPath, err := getTmpnetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpnetPath, "networks"), nil
}
