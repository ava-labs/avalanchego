// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

var (
	// Key expected to be funded for subnet-evm hardhat testing
	// TODO(marun) Remove when subnet-evm configures the genesis with this key.
	HardhatKey *secp256k1.PrivateKey

	errInsufficientNodes = errors.New("at least one node is required")
)

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

	// Id of the network. If zero, must be set in Genesis. Consider
	// using the GetNetworkID method if needing to retrieve the ID of
	// a running network.
	NetworkID uint32

	// Configuration common across nodes

	// Genesis for the network. If nil, NetworkID must be non-zero
	Genesis *genesis.UnparsedConfig

	// Configuration for primary network chains (P, X, C)
	// TODO(marun) Rename to PrimaryChainConfigs
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

func NewDefaultNetwork(owner string) *Network {
	return &Network{
		Owner: owner,
		Nodes: NewNodesOrPanic(DefaultNodeCount),
	}
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

func BootstrapNewNetwork(
	ctx context.Context,
	w io.Writer,
	network *Network,
	rootNetworkDir string,
	avalancheGoExecPath string,
	pluginDir string,
) error {
	if len(network.Nodes) == 0 {
		return errInsufficientNodes
	}
	if err := checkVMBinaries(w, network.Subnets, avalancheGoExecPath, pluginDir); err != nil {
		return err
	}
	if err := network.EnsureDefaultConfig(w, avalancheGoExecPath, pluginDir); err != nil {
		return err
	}
	if err := network.Create(rootNetworkDir); err != nil {
		return err
	}
	return network.Bootstrap(ctx, w)
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
func (n *Network) EnsureDefaultConfig(w io.Writer, avalancheGoPath string, pluginDir string) error {
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
	n.DefaultFlags.SetDefaults(DefaultTmpnetFlags())

	if len(n.Nodes) == 1 {
		// Sybil protection needs to be disabled for a single node network to start
		n.DefaultFlags[config.SybilProtectionEnabledKey] = false
	}

	// Only configure the plugin dir with a non-empty value to ensure
	// the use of the default value (`[datadir]/plugins`) when
	// no plugin dir is configured.
	if len(pluginDir) > 0 {
		if _, ok := n.DefaultFlags[config.PluginDirKey]; !ok {
			n.DefaultFlags[config.PluginDirKey] = pluginDir
		}
	}

	// Ensure pre-funded keys if the genesis is not predefined
	if n.Genesis == nil && len(n.PreFundedKeys) == 0 {
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
	pluginDir, err := n.getPluginDir()
	if err != nil {
		return err
	}
	if len(pluginDir) > 0 {
		if err := os.MkdirAll(pluginDir, perms.ReadWriteExecute); err != nil {
			return fmt.Errorf("failed to create plugin dir: %w", err)
		}
	}

	if n.NetworkID == 0 && n.Genesis == nil {
		genesis, err := n.DefaultGenesis()
		if err != nil {
			return err
		}
		n.Genesis = genesis
	}

	for _, node := range n.Nodes {
		// Ensure the node is configured for use with the network and
		// knows where to write its configuration.
		if err := n.EnsureNodeConfig(node); err != nil {
			return err
		}
	}

	// Ensure configuration on disk is current
	return n.Write()
}

func (n *Network) DefaultGenesis() (*genesis.UnparsedConfig, error) {
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

	return NewTestGenesis(defaultNetworkID, n.Nodes, keysToFund)
}

// Starts the specified nodes
func (n *Network) StartNodes(ctx context.Context, w io.Writer, nodesToStart ...*Node) error {
	if len(nodesToStart) == 0 {
		return errInsufficientNodes
	}
	nodesToWaitFor := nodesToStart
	if !slices.Contains(nodesToStart, n.Nodes[0]) {
		// If starting all nodes except the bootstrap node (because the bootstrap node is already
		// running), ensure that the health of the bootstrap node will be logged by including it in
		// the set of nodes to wait for.
		nodesToWaitFor = n.Nodes
	} else {
		// Simplify output by only logging network start when starting all nodes or when starting
		// the first node by itself to bootstrap subnet creation.
		if _, err := fmt.Fprintf(w, "Starting network %s (UUID: %s)\n", n.Dir, n.UUID); err != nil {
			return err
		}
	}

	// Record the time before nodes are started to ensure visibility of subsequently collected metrics via the emitted link
	startTime := time.Now()

	// Configure the networking for each node and start
	for _, node := range nodesToStart {
		if err := n.StartNode(ctx, w, node); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprint(w, "Waiting for nodes to report healthy...\n\n"); err != nil {
		return err
	}
	if err := waitForHealthy(ctx, w, nodesToWaitFor); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\nStarted network %s (UUID: %s)\n", n.Dir, n.UUID); err != nil {
		return err
	}
	// Provide a link to the main dashboard filtered by the uuid and showing results from now till whenever the link is viewed
	startTimeStr := strconv.FormatInt(startTime.UnixMilli(), 10)
	metricsURL := MetricsLinkForNetwork(n.UUID, startTimeStr, "")
	if _, err := fmt.Fprintf(w, "\nMetrics: %s\n", metricsURL); err != nil {
		return err
	}

	return nil
}

// Start the network for the first time
func (n *Network) Bootstrap(ctx context.Context, w io.Writer) error {
	if len(n.Subnets) == 0 {
		// Without the need to coordinate subnet configuration,
		// starting all nodes at once is the simplest option.
		return n.StartNodes(ctx, w, n.Nodes...)
	}

	// The node that will be used to create subnets and bootstrap the network
	bootstrapNode := n.Nodes[0]

	// Whether sybil protection will need to be re-enabled after subnet creation
	reEnableSybilProtection := false

	if len(n.Nodes) > 1 {
		// Reduce the cost of subnet creation for a network of multiple nodes by
		// creating subnets with a single node with sybil protection
		// disabled. This allows the creation of initial subnet state without
		// requiring coordination between multiple nodes.

		if _, err := fmt.Fprintln(w, "Starting a single-node network with sybil protection disabled for quicker subnet creation"); err != nil {
			return err
		}

		// If sybil protection is enabled, it should be re-enabled before the node is used to bootstrap the other nodes
		var err error
		reEnableSybilProtection, err = bootstrapNode.Flags.GetBoolVal(config.SybilProtectionEnabledKey, true)
		if err != nil {
			return fmt.Errorf("failed to read sybil protection flag: %w", err)
		}

		// Ensure sybil protection is disabled for the bootstrap node.
		bootstrapNode.Flags[config.SybilProtectionEnabledKey] = false
	}

	if err := n.StartNodes(ctx, w, bootstrapNode); err != nil {
		return err
	}

	// Don't restart the node during subnet creation since it will always be restarted afterwards.
	if err := n.CreateSubnets(ctx, w, bootstrapNode.URI, false /* restartRequired */); err != nil {
		return err
	}

	if reEnableSybilProtection {
		if _, err := fmt.Fprintf(w, "Re-enabling sybil protection for %s\n", bootstrapNode.NodeID); err != nil {
			return err
		}
		delete(bootstrapNode.Flags, config.SybilProtectionEnabledKey)
	}

	if _, err := fmt.Fprintf(w, "Restarting bootstrap node %s\n", bootstrapNode.NodeID); err != nil {
		return err
	}

	if len(n.Nodes) == 1 {
		// Ensure the node is restarted to pick up subnet and chain configuration
		return n.RestartNode(ctx, w, bootstrapNode)
	}

	// TODO(marun) This last restart of the bootstrap node might be unnecessary if:
	// - sybil protection didn't change
	// - the node is not a subnet validator

	// Ensure the bootstrap node is restarted to pick up configuration changes. Avoid using
	// RestartNode since the node won't be able to report healthy until other nodes are started.
	if err := bootstrapNode.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop node %s: %w", bootstrapNode.NodeID, err)
	}
	if err := n.StartNode(ctx, w, bootstrapNode); err != nil {
		return fmt.Errorf("failed to start node %s: %w", bootstrapNode.NodeID, err)
	}

	if _, err := fmt.Fprintln(w, "Starting remaining nodes..."); err != nil {
		return err
	}
	return n.StartNodes(ctx, w, n.Nodes[1:]...)
}

// Starts the provided node after configuring it for the network.
func (n *Network) StartNode(ctx context.Context, w io.Writer, node *Node) error {
	// This check is duplicative for a network that is starting, but ensures
	// that individual node start/restart won't fail due to missing binaries.
	pluginDir, err := n.getPluginDir()
	if err != nil {
		return err
	}

	if err := n.EnsureNodeConfig(node); err != nil {
		return err
	}

	// Check the VM binaries after EnsureNodeConfig to ensure node.RuntimeConfig is non-nil
	if err := checkVMBinaries(w, n.Subnets, node.RuntimeConfig.AvalancheGoPath, pluginDir); err != nil {
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

// Restart a single node.
func (n *Network) RestartNode(ctx context.Context, w io.Writer, node *Node) error {
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
	return WaitForHealthy(ctx, node)
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
	if _, err := fmt.Fprintln(w, " restarting network"); err != nil {
		return err
	}
	for _, node := range n.Nodes {
		if err := n.RestartNode(ctx, w, node); err != nil {
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
	networkID := n.GetNetworkID()
	if networkID > 0 {
		// Convert the network id to a string to ensure consistency in JSON round-tripping.
		flags[config.NetworkNameKey] = strconv.FormatUint(uint64(networkID), 10)
	}

	if err := node.EnsureKeys(); err != nil {
		return err
	}

	flags.SetDefaults(n.DefaultFlags)

	// Set fields including the network path
	if len(n.Dir) > 0 {
		defaultFlags := FlagsMap{
			config.ChainConfigDirKey: n.GetChainConfigDir(),
		}

		if n.Genesis != nil {
			defaultFlags[config.GenesisFileKey] = n.getGenesisPath()
		}

		// Only set the subnet dir if it exists or the node won't start.
		subnetDir := n.GetSubnetDir()
		if _, err := os.Stat(subnetDir); err == nil {
			defaultFlags[config.SubnetConfigDirKey] = subnetDir
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		node.Flags.SetDefaults(defaultFlags)

		// Ensure the node's data dir is configured
		dataDir := node.GetDataDir()
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

// TrackedSubnetsForNode returns the subnet IDs for the given node
func (n *Network) TrackedSubnetsForNode(nodeID ids.NodeID) string {
	subnetIDs := make([]string, 0, len(n.Subnets))
	for _, subnet := range n.Subnets {
		if subnet.SubnetID == ids.Empty {
			// Subnet has not yet been created
			continue
		}
		// Only track subnets that this node validates
		for _, validatorID := range subnet.ValidatorIDs {
			if validatorID == nodeID {
				subnetIDs = append(subnetIDs, subnet.SubnetID.String())
				break
			}
		}
	}
	return strings.Join(subnetIDs, ",")
}

func (n *Network) GetSubnet(name string) *Subnet {
	for _, subnet := range n.Subnets {
		if subnet.Name == name {
			return subnet
		}
	}
	return nil
}

// Ensure that each subnet on the network is created. If restartRequired is false, node restart
// to pick up configuration changes becomes the responsibility of the caller.
func (n *Network) CreateSubnets(ctx context.Context, w io.Writer, apiURI string, restartRequired bool) error {
	createdSubnets := make([]*Subnet, 0, len(n.Subnets))
	for _, subnet := range n.Subnets {
		if len(subnet.ValidatorIDs) == 0 {
			return fmt.Errorf("subnet %s needs at least one validator", subnet.SubnetID)
		}
		if subnet.SubnetID != ids.Empty {
			// The subnet already exists
			continue
		}

		if _, err := fmt.Fprintf(w, "Creating subnet %q\n", subnet.Name); err != nil {
			return err
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
		if err := subnet.Write(n.GetSubnetDir(), n.GetChainConfigDir()); err != nil {
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

	// Ensure the pre-funded key changes are persisted to disk
	if err := n.Write(); err != nil {
		return err
	}

	reconfiguredNodes := []*Node{}
	for _, node := range n.Nodes {
		existingTrackedSubnets, err := node.Flags.GetStringVal(config.TrackSubnetsKey)
		if err != nil {
			return err
		}
		trackedSubnets := n.TrackedSubnetsForNode(node.NodeID)
		if existingTrackedSubnets == trackedSubnets {
			continue
		}
		node.Flags[config.TrackSubnetsKey] = trackedSubnets
		reconfiguredNodes = append(reconfiguredNodes, node)
	}

	if restartRequired {
		if _, err := fmt.Fprintln(w, "Restarting node(s) to enable them to track the new subnet(s)"); err != nil {
			return err
		}

		for _, node := range reconfiguredNodes {
			if len(node.URI) == 0 {
				// Only running nodes should be restarted
				continue
			}
			if err := n.RestartNode(ctx, w, node); err != nil {
				return err
			}
		}
	}

	// Add validators for the subnet
	for _, subnet := range createdSubnets {
		if _, err := fmt.Fprintf(w, "Adding validators for subnet %q\n", subnet.Name); err != nil {
			return err
		}

		// Collect the nodes intended to validate the subnet
		validatorIDs := set.NewSet[ids.NodeID](len(subnet.ValidatorIDs))
		validatorIDs.Add(subnet.ValidatorIDs...)
		validatorNodes := []*Node{}
		for _, node := range n.Nodes {
			if !validatorIDs.Contains(node.NodeID) {
				continue
			}
			validatorNodes = append(validatorNodes, node)
		}

		if err := subnet.AddValidators(ctx, w, apiURI, validatorNodes...); err != nil {
			return err
		}
	}

	// Wait for nodes to become subnet validators
	pChainClient := platformvm.NewClient(n.Nodes[0].URI)
	validatorsToRestart := set.Set[ids.NodeID]{}
	for _, subnet := range createdSubnets {
		if err := WaitForActiveValidators(ctx, w, pChainClient, subnet); err != nil {
			return err
		}

		// It should now be safe to create chains for the subnet
		if err := subnet.CreateChains(ctx, w, n.Nodes[0].URI); err != nil {
			return err
		}

		// Persist the chain configuration
		if err := subnet.Write(n.GetSubnetDir(), n.GetChainConfigDir()); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, " wrote chain configuration for subnet %q\n", subnet.Name); err != nil {
			return err
		}

		// If one or more of the subnets chains have explicit configuration, the
		// subnet's validator nodes will need to be restarted for those nodes to read
		// the newly written chain configuration and apply it to the chain(s).
		if subnet.HasChainConfig() {
			validatorsToRestart.Add(subnet.ValidatorIDs...)
		}
	}

	if !restartRequired || len(validatorsToRestart) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(w, "Restarting node(s) to pick up chain configuration"); err != nil {
		return err
	}

	// Restart nodes to allow configuration for the new chains to take effect
	for _, node := range n.Nodes {
		if !validatorsToRestart.Contains(node.NodeID) {
			continue
		}
		if err := n.RestartNode(ctx, w, node); err != nil {
			return err
		}
	}

	return nil
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

// GetNetworkID returns the effective ID of the network. If the network
// defines a genesis, the network ID in the genesis will be returned. If a
// genesis is not present (i.e. a network with a genesis included in the
// avalanchego binary - mainnet, testnet and local), the value of the
// NetworkID field will be returned
func (n *Network) GetNetworkID() uint32 {
	if n.Genesis != nil && n.Genesis.NetworkID > 0 {
		return n.Genesis.NetworkID
	}
	return n.NetworkID
}

func (n *Network) getPluginDir() (string, error) {
	return n.DefaultFlags.GetStringVal(config.PluginDirKey)
}

// Waits until the provided nodes are healthy.
func waitForHealthy(ctx context.Context, w io.Writer, nodes []*Node) error {
	ticker := time.NewTicker(networkHealthCheckInterval)
	defer ticker.Stop()

	unhealthyNodes := set.Of(nodes...)
	for {
		for node := range unhealthyNodes {
			healthy, err := node.IsHealthy(ctx)
			if err != nil {
				return err
			}
			if !healthy {
				continue
			}

			unhealthyNodes.Remove(node)
			if _, err := fmt.Fprintf(w, "%s is healthy @ %s\n", node.NodeID, node.URI); err != nil {
				return err
			}
		}

		if unhealthyNodes.Len() == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see all nodes healthy before timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
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

// Retrieves the path to a reusable network path for the given owner.
func GetReusableNetworkPathForOwner(owner string) (string, error) {
	networkPath, err := getDefaultRootNetworkDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(networkPath, "latest_"+owner), nil
}

const invalidRPCVersion = 0

// checkVMBinaries checks that VM binaries for the given subnets exist and optionally checks that VM
// binaries have the same rpcchainvm version as the indicated avalanchego binary.
func checkVMBinaries(w io.Writer, subnets []*Subnet, avalanchegoPath string, pluginDir string) error {
	if len(subnets) == 0 {
		return nil
	}

	expectedRPCVersion, err := getRPCVersion(avalanchegoPath, "--version-json")
	if err != nil {
		// Only warn if the rpc version is not available to ensure backwards compatibility.
		if _, err := fmt.Fprintf(w, "Warning: Unable to check rpcchainvm version for avalanchego: %v\n", err); err != nil {
			return err
		}
	}

	errs := []error{}
	for _, subnet := range subnets {
		for _, chain := range subnet.Chains {
			pluginPath := filepath.Join(pluginDir, chain.VMID.String())

			// Check that the path exists
			if _, err := os.Stat(pluginPath); err != nil {
				errs = append(errs, fmt.Errorf("failed to check VM binary for subnet %q: %w", subnet.Name, err))
			}

			if len(chain.VersionArgs) == 0 || expectedRPCVersion == invalidRPCVersion {
				// Not possible to check the rpcchainvm version
				continue
			}

			// Check that the VM's rpcchainvm version matches avalanchego's version
			rpcVersion, err := getRPCVersion(pluginPath, chain.VersionArgs...)
			if err != nil {
				if _, err := fmt.Fprintf(w, "Warning: Unable to check rpcchainvm version for VM Binary for subnet %q: %v\n", subnet.Name, err); err != nil {
					return err
				}
			} else if expectedRPCVersion != rpcVersion {
				errs = append(errs, fmt.Errorf("unexpected rpcchainvm version for VM binary of subnet %q: %q reports %d, but %q reports %d", subnet.Name, avalanchegoPath, expectedRPCVersion, pluginPath, rpcVersion))
			}
		}
	}

	return errors.Join(errs...)
}

type RPCChainVMVersion struct {
	RPCChainVM uint64 `json:"rpcchainvm"`
}

// getRPCVersion attempts to invoke the given command with the specified version arguments and
// retrieve an rpcchainvm version from its output.
func getRPCVersion(command string, versionArgs ...string) (uint64, error) {
	cmd := exec.Command(command, versionArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("command %q failed with output: %s", command, output)
	}
	version := &RPCChainVMVersion{}
	if err := json.Unmarshal(output, version); err != nil {
		return 0, fmt.Errorf("failed to unmarshal output from command %q: %w, output: %s", command, err, output)
	}

	return version.RPCChainVM, nil
}

// MetricsLinkForNetwork returns a link to the default metrics dashboard for the network
// with the given UUID. The start and end times are accepted as strings to support the
// use of Grafana's time range syntax (e.g. `now`, `now-1h`).
func MetricsLinkForNetwork(networkUUID string, startTime string, endTime string) string {
	if startTime == "" {
		startTime = "now-1h"
	}
	if endTime == "" {
		endTime = "now"
	}
	return fmt.Sprintf(
		"https://grafana-poc.avax-dev.network/d/kBQpRdWnk/avalanche-main-dashboard?&var-filter=network_uuid%%7C%%3D%%7C%s&var-filter=is_ephemeral_node%%7C%%3D%%7Cfalse&from=%s&to=%s",
		networkUUID,
		startTime,
		endTime,
	)
}
