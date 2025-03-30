// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
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

	// Message to log indicating where to look for metrics and logs for network
	MetricsAvailableMessage = "metrics and logs available via grafana (collectors must be running)"

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

	Log logging.Logger
}

func NewDefaultNetwork(log logging.Logger, owner string) *Network {
	return &Network{
		UUID:  uuid.NewString(),
		Owner: owner,
		Nodes: NewNodesOrPanic(DefaultNodeCount),
		Log:   log,
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
	log logging.Logger,
	network *Network,
	rootNetworkDir string,
) error {
	if len(network.Nodes) == 0 {
		return errInsufficientNodes
	}
	if len(network.DefaultRuntimeConfig.AvalancheGoPath) > 0 {
		pluginDir, err := network.GetPluginDir()
		if err != nil {
			return err
		}
		if err := checkVMBinaries(log, network.Subnets, network.DefaultRuntimeConfig.AvalancheGoPath, pluginDir); err != nil {
			return err
		}
	}
	if err := network.EnsureDefaultConfig(log); err != nil {
		return err
	}
	if err := network.Create(rootNetworkDir); err != nil {
		return err
	}
	return network.Bootstrap(ctx, log)
}

// Stops the nodes of the network configured in the provided directory.
func StopNetwork(ctx context.Context, dir string) error {
	network, err := ReadNetwork(ctx, dir)
	if err != nil {
		return err
	}
	return network.Stop(ctx)
}

// Restarts the nodes of the network configured in the provided directory.
func RestartNetwork(ctx context.Context, log logging.Logger, dir string) error {
	network, err := ReadNetwork(ctx, dir)
	if err != nil {
		return err
	}
	return network.Restart(ctx, log)
}

// Reads a network from the provided directory.
func ReadNetwork(ctx context.Context, dir string) (*Network, error) {
	canonicalDir, err := toCanonicalDir(dir)
	if err != nil {
		return nil, err
	}
	network := &Network{
		Dir: canonicalDir,
	}
	if err := network.Read(ctx); err != nil {
		return nil, fmt.Errorf("failed to read network: %w", err)
	}
	return network, nil
}

// Initializes a new network with default configuration.
func (n *Network) EnsureDefaultConfig(log logging.Logger) error {
	n.Log = log

	n.Log.Info("preparing configuration for new network")
	// TODO(marun) Provide more detail about configuration of the new network?

	// A UUID supports centralized metrics collection
	if len(n.UUID) == 0 {
		n.UUID = uuid.NewString()
	}

	if len(n.Nodes) == 1 {
		// Sybil protection needs to be disabled for a single node network to start
		n.DefaultFlags[config.SybilProtectionEnabledKey] = false
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

	// TODO(marun) Make this part of the node process
	if len(n.DefaultRuntimeConfig.AvalancheGoPath) > 0 {
		// Ensure the existence of the plugin directory or nodes won't be able to start.
		pluginDir, err := n.GetPluginDir()
		if err != nil {
			return err
		}
		if len(pluginDir) > 0 {
			if err := os.MkdirAll(pluginDir, perms.ReadWriteExecute); err != nil {
				return fmt.Errorf("failed to create plugin dir: %w", err)
			}
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
func (n *Network) StartNodes(ctx context.Context, log logging.Logger, nodesToStart ...*Node) error {
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
		log.Info("starting network",
			zap.String("networkDir", n.Dir),
			zap.String("uuid", n.UUID),
		)
	}

	// Record the time before nodes are started to ensure visibility of subsequently collected metrics via the emitted link
	startTime := time.Now()

	// Configure the networking for each node and start
	for _, node := range nodesToStart {
		if err := n.StartNode(ctx, node); err != nil {
			return err
		}
	}

	log.Info("waiting for nodes to report healthy")
	if err := waitForHealthy(ctx, nodesToWaitFor); err != nil {
		return err
	}
	log.Info("started network",
		zap.String("networkDir", n.Dir),
		zap.String("uuid", n.UUID),
	)
	// Provide a link to the main dashboard filtered by the uuid and showing results from now till whenever the link is viewed
	startTimeStr := strconv.FormatInt(startTime.UnixMilli(), 10)
	metricsURL := MetricsLinkForNetwork(n.UUID, startTimeStr, "")

	// Write link to the network path
	metricsPath := filepath.Join(n.Dir, "metrics.txt")
	if err := os.WriteFile(metricsPath, []byte(metricsURL+"\n"), perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write metrics link to %s: %w", metricsPath, err)
	}

	log.Info(MetricsAvailableMessage,
		zap.String("url", metricsURL),
		zap.String("linkPath", metricsPath),
	)

	return nil
}

// Start the network for the first time
func (n *Network) Bootstrap(ctx context.Context, log logging.Logger) error {
	if len(n.Subnets) == 0 {
		// Without the need to coordinate subnet configuration,
		// starting all nodes at once is the simplest option.
		return n.StartNodes(ctx, log, n.Nodes...)
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

		log.Info("starting a single-node network with sybil protection disabled for quicker subnet creation")

		// If sybil protection is enabled, it should be re-enabled before the node is used to bootstrap the other nodes
		var err error
		reEnableSybilProtection, err = bootstrapNode.Flags.GetBoolVal(config.SybilProtectionEnabledKey, true)
		if err != nil {
			return fmt.Errorf("failed to read sybil protection flag: %w", err)
		}

		// Ensure sybil protection is disabled for the bootstrap node.
		bootstrapNode.Flags[config.SybilProtectionEnabledKey] = false
	}

	if err := n.StartNodes(ctx, log, bootstrapNode); err != nil {
		return err
	}

	// Don't restart the node during subnet creation since it will always be restarted afterwards.
	uri, cancel, err := bootstrapNode.GetLocalURI(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if err := n.CreateSubnets(ctx, log, uri, false /* restartRequired */); err != nil {
		return err
	}

	if reEnableSybilProtection {
		log.Info("re-enabling sybil protection",
			zap.Stringer("nodeID", bootstrapNode.NodeID),
		)
		delete(bootstrapNode.Flags, config.SybilProtectionEnabledKey)
	}

	log.Info("restarting bootstrap node",
		zap.Stringer("nodeID", bootstrapNode.NodeID),
	)

	// Ensure the node is restarted to pick up subnet and chain configuration
	//
	// TODO(marun) This restart of the bootstrap node might be unnecessary if:
	// - sybil protection didn't change
	// - the node is not a subnet validator
	if err := n.RestartNodes(ctx, bootstrapNode); err != nil {
		return err
	}

	if len(n.Nodes) == 1 {
		return nil
	}

	log.Info("starting remaining nodes")
	return n.StartNodes(ctx, log, n.Nodes[1:]...)
}

// Starts the provided node after configuring it for the network.
func (n *Network) StartNode(ctx context.Context, node *Node) error {
	if err := n.EnsureNodeConfig(node); err != nil {
		return err
	}

	bootstrapIPs, bootstrapIDs, err := n.getBootstrapIPsAndIDs(ctx, node)
	if err != nil {
		return err
	}
	node.Flags[config.BootstrapIDsKey] = strings.Join(bootstrapIDs, ",")
	node.Flags[config.BootstrapIPsKey] = strings.Join(bootstrapIPs, ",")

	if err := node.Write(); err != nil {
		return err
	}

	if err := node.Start(ctx); err != nil {
		// Attempt to stop an unhealthy node to provide some assurance to the caller
		// that an error condition will not result in a lingering process.
		err = errors.Join(err, node.Stop(ctx))
		return err
	}

	return nil
}

// Restart the provided nodes. Blocks on the nodes accepting API requests but not their health.
//
// TODO(marun) This no longer needs to be a method of Network - fold
// it into the node restart method.
func (n *Network) RestartNodes(ctx context.Context, nodes ...*Node) error {
	for _, node := range nodes {
		// TODO(marun) Using the content fields requires that they always be maintained on
		// start/restart (vs on disk where they are always current). Refactor so this is
		// cleaner.
		if err := n.EnsureNodeConfig(node); err != nil {
			return fmt.Errorf("failed to ensure node configuration: %w", err)
		}
		if err := node.Restart(ctx); err != nil {
			return fmt.Errorf("failed to restart node %s: %w", node.NodeID, err)
		}
	}
	return nil
}

// Stops all nodes in the network.
func (n *Network) Stop(ctx context.Context) error {
	// Target all nodes, including the ephemeral ones
	nodes, err := ReadNodes(ctx, n, true /* includeEphemeral */)
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
func (n *Network) Restart(ctx context.Context, log logging.Logger) error {
	log.Info("restarting network")
	if err := n.RestartNodes(ctx, n.Nodes...); err != nil {
		return err
	}
	return WaitForHealthyNodes(ctx, log, n.Nodes...)
}

// Waits for the provided nodes to become healthy.
func WaitForHealthyNodes(ctx context.Context, log logging.Logger, nodes ...*Node) error {
	for _, node := range nodes {
		log.Info("waiting for node to become healthy",
			zap.Stringer("nodeID", node.NodeID),
		)
		if err := WaitForHealthyNode(ctx, log, node); err != nil {
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

	// Ensure the node has access to network configuration
	node.network = n

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

	chainConfigContent, err := n.GetChainConfigContent()
	if err != nil {
		return err
	}

	// Supplying this as part of initial configuration is not
	// supported since tmpnet takes responsibility for chain creation
	// and the chain IDs necessary for the configuration to be valid
	// won't be known until chain creation.
	flags[config.ChainConfigContentKey] = chainConfigContent

	defaultFlags := FlagsMap{}
	if n.Genesis != nil {
		genesisFileContent, err := n.GetGenesisFileContent()
		if err != nil {
			return err
		}
		defaultFlags[config.GenesisFileContentKey] = genesisFileContent
	}
	flags.SetDefaults(defaultFlags)

	// TODO(marun) Support providing subnet configuration as content
	// // Only set the subnet dir if it exists or the node won't start.
	// subnetDir := n.GetSubnetDir()
	// if _, err := os.Stat(subnetDir); err == nil {
	// 	defaultFlags[config.SubnetConfigDirKey] = subnetDir
	// } else if !errors.Is(err, os.ErrNotExist) {
	// 	return err
	// }

	// Ensure the node runtime is configured
	if node.RuntimeConfig == nil {
		node.RuntimeConfig = &n.DefaultRuntimeConfig
	}

	// Ensure the node's data dir is configured
	if len(n.Dir) > 0 && len(node.GetDataDir()) == 0 {
		node.Flags[config.DataDirKey] = filepath.Join(n.Dir, node.NodeID.String())
	}

	// Set the runtime defaults last to ensure they can be overridden
	node.getRuntime().SetDefaultFlags()

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
func (n *Network) CreateSubnets(ctx context.Context, log logging.Logger, apiURI string, restartRequired bool) error {
	createdSubnets := make([]*Subnet, 0, len(n.Subnets))
	for _, subnet := range n.Subnets {
		if len(subnet.ValidatorIDs) == 0 {
			return fmt.Errorf("subnet %s needs at least one validator", subnet.SubnetID)
		}
		if subnet.SubnetID != ids.Empty {
			// The subnet already exists
			continue
		}

		log.Info("creating subnet",
			zap.String("name", subnet.Name),
		)

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
		if err := subnet.Create(ctx, apiURI); err != nil {
			return err
		}

		log.Info("created subnet",
			zap.String("name", subnet.Name),
			zap.Stringer("id", subnet.SubnetID),
		)

		// Persist the subnet configuration
		if err := subnet.Write(n.GetSubnetDir(), n.GetChainConfigDir()); err != nil {
			return err
		}

		log.Info("wrote subnet configuration",
			zap.String("name", subnet.Name),
		)

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
		log.Info("restarting node(s) to enable them to track the new subnet(s)")

		runningNodes := make([]*Node, 0, len(reconfiguredNodes))
		for _, node := range reconfiguredNodes {
			if len(node.URI) == 0 {
				runningNodes = append(runningNodes, node)
			}
		}

		if err := n.RestartNodes(ctx, runningNodes...); err != nil {
			return err
		}

		if err := WaitForHealthyNodes(ctx, log, runningNodes...); err != nil {
			return err
		}
	}

	// Add validators for the subnet
	for _, subnet := range createdSubnets {
		log.Info("adding validators for subnet",
			zap.String("name", subnet.Name),
		)

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

		if err := subnet.AddValidators(ctx, log, apiURI, validatorNodes...); err != nil {
			return err
		}
	}

	// Wait for nodes to become subnet validators
	pChainClient := platformvm.NewClient(apiURI)
	validatorsToRestart := set.Set[ids.NodeID]{}
	for _, subnet := range createdSubnets {
		if err := WaitForActiveValidators(ctx, log, pChainClient, subnet); err != nil {
			return err
		}

		// It should now be safe to create chains for the subnet
		if err := subnet.CreateChains(ctx, log, apiURI); err != nil {
			return err
		}

		// Persist the chain configuration
		if err := subnet.Write(n.GetSubnetDir(), n.GetChainConfigDir()); err != nil {
			return err
		}
		log.Info("wrote chain configuration for subnet",
			zap.String("name", subnet.Name),
		)

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

	log.Info("restarting node(s) to pick up chain configuration")

	// Restart nodes to allow configuration for the new chains to take effect
	nodesToRestart := make([]*Node, 0, len(n.Nodes))
	for _, node := range n.Nodes {
		if validatorsToRestart.Contains(node.NodeID) {
			nodesToRestart = append(nodesToRestart, node)
		}
	}

	if err := n.RestartNodes(ctx, nodesToRestart...); err != nil {
		return err
	}

	if err := WaitForHealthyNodes(ctx, log, nodesToRestart...); err != nil {
		return err
	}

	return nil
}

func (n *Network) GetNode(nodeID ids.NodeID) (*Node, error) {
	for _, node := range n.Nodes {
		if node.NodeID == nodeID {
			return node, nil
		}
	}
	return nil, fmt.Errorf("%s is not known to the network", nodeID)
}

func (n *Network) GetNodeURIs() []NodeURI {
	return GetNodeURIs(n.Nodes)
}

// Retrieves bootstrap IPs and IDs for all nodes except the skipped one (this supports
// collecting the bootstrap details for restarting a node).
func (n *Network) getBootstrapIPsAndIDs(ctx context.Context, skippedNode *Node) ([]string, []string, error) {
	// Collect staking addresses of non-ephemeral nodes for use in bootstrapping a node
	nodes, err := ReadNodes(ctx, n, false /* includeEphemeral */)
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

		if node.StakingAddress == (netip.AddrPort{}) {
			// Node is not running
			continue
		}

		bootstrapIPs = append(bootstrapIPs, node.StakingAddress.String())
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

// For consumption outside of avalanchego. Needs to be kept exported.
func (n *Network) GetPluginDir() (string, error) {
	return n.DefaultFlags.GetStringVal(config.PluginDirKey)
}

// Sets the plugin dir for the network. If the provided plugin dir is
// empty, the default plugin dir (`[datadir]/plugins`) will be used.
func (n *Network) SetPluginDir(pluginDir string) {
	if len(pluginDir) > 0 {
		n.DefaultFlags[config.PluginDirKey] = pluginDir
	} else {
		delete(n.DefaultFlags, config.PluginDirKey)
	}
}

func (n *Network) GetChainConfigContent() (string, error) {
	chainConfigs := map[string]chains.ChainConfig{}
	for alias, flags := range n.ChainConfigs {
		marshaledFlags, err := json.Marshal(flags)
		if err != nil {
			return "", fmt.Errorf("failed to marshal flags map for %s-Chain: %w", alias, err)
		}
		chainConfigs[alias] = chains.ChainConfig{
			Config: marshaledFlags,
		}
	}

	// TODO(marun) Make debug or remove this line
	n.Log.Info("collected primary chain configs",
		zap.Any("chainConfigs", chainConfigs),
	)

	for _, subnet := range n.Subnets {
		for _, chain := range subnet.Chains {
			if chain.ChainID == ids.Empty {
				// The chain hasn't been created yet and it's not possible to supply
				// configuration without a chain ID.
				continue
			}
			chainConfigs[chain.ChainID.String()] = chains.ChainConfig{
				Config: []byte(chain.Config),
			}
		}
	}

	// TODO(marun) Make debug or remove this line
	n.Log.Info("collected primary and subnet chains",
		zap.Any("chainConfigs", chainConfigs),
	)

	marshaledChainConfigs, err := json.Marshal(chainConfigs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chain configs: %w", err)
	}
	return base64.StdEncoding.EncodeToString(marshaledChainConfigs), nil
}

func (n *Network) GetGenesisFileContent() (string, error) {
	bytes, err := json.Marshal(n.Genesis)
	if err != nil {
		return "", fmt.Errorf("failed to marshal genesis: %w", err)
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// Waits until the provided nodes are healthy.
func waitForHealthy(ctx context.Context, nodes []*Node) error {
	// TODO(marun) Change the network health interval to be longer for kube since the nodes will take longer to start
	// Also maybe build in a wait for pod readiness?
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
			node.getNetwork().Log.Info("node is healthy",
				zap.Stringer("nodeID", node.NodeID),
				// TODO(marun) This would need to be a local URI for kube.
				zap.String("uri", node.URI),
			)
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
func checkVMBinaries(log logging.Logger, subnets []*Subnet, avalanchegoPath string, pluginDir string) error {
	if len(subnets) == 0 {
		return nil
	}

	avalanchegoRPCVersion, err := getRPCVersion(avalanchegoPath, "--version-json")
	if err != nil {
		log.Warn("unable to check rpcchainvm version for avalanchego", zap.Error(err))
		return nil
	}

	var incompatibleChains bool
	for _, subnet := range subnets {
		for _, chain := range subnet.Chains {
			vmPath := filepath.Join(pluginDir, chain.VMID.String())

			// Check that the path exists
			if _, err := os.Stat(vmPath); err != nil {
				log.Warn("unable to check rpcchainvm version for VM",
					zap.String("vmPath", vmPath),
					zap.Error(err),
				)
				continue
			}

			if len(chain.VersionArgs) == 0 || avalanchegoRPCVersion == invalidRPCVersion {
				// Not possible to check the rpcchainvm version
				continue
			}

			// Check that the VM's rpcchainvm version matches avalanchego's version
			vmRPCVersion, err := getRPCVersion(vmPath, chain.VersionArgs...)
			if err != nil {
				log.Warn("unable to check rpcchainvm version for VM Binary",
					zap.String("subnet", subnet.Name),
					zap.Error(err),
				)
			} else if avalanchegoRPCVersion != vmRPCVersion {
				log.Error("unexpected rpcchainvm version for VM binary",
					zap.String("subnet", subnet.Name),
					zap.String("avalanchegoPath", avalanchegoPath),
					zap.Uint64("avalanchegoRPCVersion", avalanchegoRPCVersion),
					zap.String("vmPath", vmPath),
					zap.Uint64("vmRPCVersion", vmRPCVersion),
				)
				incompatibleChains = true
			}
		}
	}

	if incompatibleChains {
		return errors.New("the rpcchainvm version of the VMs for one or more chains may not be compatible with the specified avalanchego binary")
	}
	return nil
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
