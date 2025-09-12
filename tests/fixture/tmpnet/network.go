// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/constants"
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
	RootNetworkDirEnvName = "TMPNET_ROOT_NETWORK_DIR"
	NetworkDirEnvName     = "TMPNET_NETWORK_DIR"

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

	// grafanaURI is remote Grafana URI
	grafanaURI = "grafana-poc.avax-dev.network"
)

var (
	// Key expected to be funded for subnet-evm hardhat testing
	// TODO(marun) Remove when subnet-evm configures the genesis with this key.
	HardhatKey *secp256k1.PrivateKey

	errInsufficientNodes    = errors.New("at least one node is required")
	errMissingRuntimeConfig = errors.New("DefaultRuntimeConfig must not be empty")

	// Labels expected to be available in the environment when running in GitHub Actions
	githubLabels = []string{
		"gh_repo",
		"gh_workflow",
		"gh_run_id",
		"gh_run_number",
		"gh_run_attempt",
		"gh_job_id",
	}
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

// ConfigMap enables defining configuration in a format appropriate
// for round-tripping through JSON back to golang structs.
type ConfigMap map[string]any

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

	// Configuration for primary subnets
	PrimarySubnetConfig ConfigMap

	// Configuration for primary network chains (P, X, C)
	PrimaryChainConfigs map[string]ConfigMap

	// Default configuration to use when creating new nodes
	DefaultFlags         FlagsMap
	DefaultRuntimeConfig NodeRuntimeConfig

	// Keys pre-funded in the genesis on both the X-Chain and the C-Chain
	PreFundedKeys []*secp256k1.PrivateKey

	// Nodes that constitute the network
	Nodes []*Node

	// Subnets that have been enabled on the network
	Subnets []*Subnet

	log logging.Logger
}

func NewDefaultNetwork(owner string) *Network {
	return &Network{
		UUID:  uuid.NewString(),
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
		return "", stacktrace.Wrap(err)
	}
	canonicalDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", stacktrace.Wrap(err)
	}
	return canonicalDir, nil
}

func BootstrapNewNetwork(
	ctx context.Context,
	log logging.Logger,
	network *Network,
	rootNetworkDir string,
) error {
	if len(network.Nodes) == 0 {
		return stacktrace.Wrap(errInsufficientNodes)
	}

	if err := checkVMBinaries(log, network.Subnets, network.DefaultRuntimeConfig.Process); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := network.EnsureDefaultConfig(ctx, log); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := network.Create(rootNetworkDir); err != nil {
		return stacktrace.Wrap(err)
	}
	return network.Bootstrap(ctx, log)
}

// Stops the nodes of the network configured in the provided directory.
func StopNetwork(ctx context.Context, log logging.Logger, dir string) error {
	network, err := ReadNetwork(ctx, log, dir)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	return network.Stop(ctx)
}

// Restarts the nodes of the network configured in the provided directory.
func RestartNetwork(ctx context.Context, log logging.Logger, dir string) error {
	network, err := ReadNetwork(ctx, log, dir)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	return network.Restart(ctx)
}

// Restart the provided nodes. Blocks on the nodes accepting API requests but not their health.
func restartNodes(ctx context.Context, nodes []*Node) error {
	for _, node := range nodes {
		if err := node.Restart(ctx); err != nil {
			return stacktrace.Errorf("failed to restart node %s: %w", node.NodeID, err)
		}
	}
	return nil
}

// Reads a network from the provided directory.
func ReadNetwork(ctx context.Context, log logging.Logger, dir string) (*Network, error) {
	canonicalDir, err := toCanonicalDir(dir)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	network := &Network{
		Dir: canonicalDir,
		log: log,
	}
	if err := network.Read(ctx); err != nil {
		return nil, stacktrace.Errorf("failed to read network: %w", err)
	}
	if network.DefaultFlags == nil {
		network.DefaultFlags = FlagsMap{}
	}
	return network, nil
}

// Initializes a new network with default configuration.
func (n *Network) EnsureDefaultConfig(ctx context.Context, log logging.Logger) error {
	// Populate runtime defaults before logging it
	if n.DefaultRuntimeConfig.Kube != nil {
		if err := n.DefaultRuntimeConfig.Kube.ensureDefaults(ctx, log); err != nil {
			return stacktrace.Wrap(err)
		}
	}

	log.Info("preparing configuration for new network",
		zap.Any("runtimeConfig", n.DefaultRuntimeConfig),
	)

	n.log = log

	// A UUID supports centralized metrics collection
	if len(n.UUID) == 0 {
		n.UUID = uuid.NewString()
	}

	if n.DefaultFlags == nil {
		n.DefaultFlags = FlagsMap{}
	}

	// Ensure pre-funded keys if the genesis is not predefined
	if n.Genesis == nil && len(n.PreFundedKeys) == 0 {
		keys, err := NewPrivateKeys(DefaultPreFundedKeyCount)
		if err != nil {
			return stacktrace.Wrap(err)
		}
		n.PreFundedKeys = keys
	}

	// Ensure primary chains are configured
	if n.PrimaryChainConfigs == nil {
		n.PrimaryChainConfigs = map[string]ConfigMap{}
	}
	defaultChainConfigs := DefaultChainConfigs()
	for alias, defaultChainConfig := range defaultChainConfigs {
		if _, ok := n.PrimaryChainConfigs[alias]; !ok {
			n.PrimaryChainConfigs[alias] = ConfigMap{}
		}
		primaryChainConfig := n.PrimaryChainConfigs[alias]
		for key, value := range defaultChainConfig {
			if _, ok := primaryChainConfig[key]; !ok {
				primaryChainConfig[key] = value
			}
		}
	}

	emptyRuntime := NodeRuntimeConfig{}
	if n.DefaultRuntimeConfig == emptyRuntime {
		return stacktrace.Wrap(errMissingRuntimeConfig)
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
			return stacktrace.Wrap(err)
		}
	}
	if err := os.MkdirAll(rootDir, perms.ReadWriteExecute); err != nil {
		return stacktrace.Errorf("failed to create root network dir: %w", err)
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
		return stacktrace.Errorf("failed to create network dir: %w", err)
	}
	canonicalDir, err := toCanonicalDir(networkDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	n.Dir = canonicalDir

	if n.NetworkID == 0 && n.Genesis == nil {
		genesis, err := n.DefaultGenesis()
		if err != nil {
			return stacktrace.Wrap(err)
		}
		n.Genesis = genesis
	}

	for _, node := range n.Nodes {
		// Ensure the node is configured for use with the network and
		// knows where to write its configuration.
		if err := n.EnsureNodeConfig(node); err != nil {
			return stacktrace.Wrap(err)
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
		return stacktrace.Wrap(errInsufficientNodes)
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

	for _, node := range nodesToStart {
		if err := n.StartNode(ctx, node); err != nil {
			return stacktrace.Wrap(err)
		}
	}

	log.Info("waiting for nodes to report healthy")
	if err := waitForHealthy(ctx, log, nodesToWaitFor); err != nil {
		return stacktrace.Wrap(err)
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
		return stacktrace.Errorf("failed to write metrics link to %s: %w", metricsPath, err)
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

	// An existing sybil protection value that may need to be restored after subnet creation
	var existingSybilProtectionValue *string

	if len(n.Nodes) > 1 {
		// Reduce the cost of subnet creation for a network of multiple nodes by
		// creating subnets with a single node with sybil protection
		// disabled. This allows the creation of initial subnet state without
		// requiring coordination between multiple nodes.

		log.Info("starting a single-node network with sybil protection disabled for quicker subnet creation")

		// If sybil protection is enabled, it should be re-enabled before the node is used to bootstrap the other nodes
		if value, ok := bootstrapNode.Flags[config.SybilProtectionEnabledKey]; ok {
			existingSybilProtectionValue = &value
		}
		// Ensure sybil protection is disabled for the bootstrap node.
		bootstrapNode.Flags[config.SybilProtectionEnabledKey] = "false"
	}

	if err := n.StartNodes(ctx, log, bootstrapNode); err != nil {
		return stacktrace.Wrap(err)
	}

	// Don't restart the node during subnet creation since it will always be restarted afterwards.
	// uri := bootstrapNode.GetAccessibleURI()
	if err := n.CreateSubnets(ctx, log, bootstrapNode, true /* restartRequired */); err != nil {
		return stacktrace.Wrap(err)
	}

	if existingSybilProtectionValue == nil {
		log.Info("re-enabling sybil protection",
			zap.Stringer("nodeID", bootstrapNode.NodeID),
		)
		delete(bootstrapNode.Flags, config.SybilProtectionEnabledKey)
	} else {
		log.Info("restoring previous sybil protection value",
			zap.Stringer("nodeID", bootstrapNode.NodeID),
			zap.String("sybilProtectionEnabled", *existingSybilProtectionValue),
		)
		bootstrapNode.Flags[config.SybilProtectionEnabledKey] = *existingSybilProtectionValue
	}

	// Ensure the bootstrap node is restarted to pick up subnet and chain configuration
	//
	// TODO(marun) This restart might be unnecessary if:
	// - sybil protection didn't change
	// - the node is not a subnet validator
	log.Info("restarting bootstrap node",
		zap.Stringer("nodeID", bootstrapNode.NodeID),
	)
	if err := bootstrapNode.Restart(ctx); err != nil {
		return stacktrace.Wrap(err)
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
		return stacktrace.Wrap(err)
	}
	if err := node.Write(); err != nil {
		return stacktrace.Wrap(err)
	}

	if err := node.Start(ctx); err != nil {
		// Attempt to stop an unhealthy node to provide some assurance to the caller
		// that an error condition will not result in a lingering process.
		err = errors.Join(err, node.Stop(ctx))
		return stacktrace.Wrap(err)
	}

	return nil
}

// Stops all nodes in the network.
func (n *Network) Stop(ctx context.Context) error {
	// Ensure the node state is up-to-date
	if err := n.readNodes(ctx); err != nil {
		return stacktrace.Wrap(err)
	}

	var errs []error

	// Initiate stop on all nodes
	for _, node := range n.Nodes {
		if err := node.InitiateStop(ctx); err != nil {
			errs = append(errs, stacktrace.Errorf("failed to stop node %s: %w", node.NodeID, err))
		}
	}

	// Wait for stop to complete on all nodes
	for _, node := range n.Nodes {
		if err := node.WaitForStopped(ctx); err != nil {
			errs = append(errs, stacktrace.Errorf("failed to wait for node %s to stop: %w", node.NodeID, err))
		}
	}

	if len(errs) > 0 {
		return stacktrace.Errorf("failed to stop network:\n%w", errors.Join(errs...))
	}
	return nil
}

// Restarts all running nodes in the network.
func (n *Network) Restart(ctx context.Context) error {
	n.log.Info("restarting network")
	nodes := make([]*Node, 0, len(n.Nodes))
	for _, node := range n.Nodes {
		if !node.IsRunning() {
			continue
		}
		nodes = append(nodes, node)
	}
	if err := restartNodes(ctx, nodes); err != nil {
		return stacktrace.Wrap(err)
	}
	return WaitForHealthyNodes(ctx, n.log, nodes)
}

// Waits for the provided nodes to become healthy.
func WaitForHealthyNodes(ctx context.Context, log logging.Logger, nodes []*Node) error {
	for _, node := range nodes {
		log.Info("waiting for node to become healthy",
			zap.Stringer("nodeID", node.NodeID),
		)
		if err := node.WaitForHealthy(ctx); err != nil {
			return stacktrace.Wrap(err)
		}
	}
	return nil
}

// Ensures the provided node has the configuration it needs to start. If the data dir is not
// set, it will be defaulted to [nodeParentDir]/[node ID].
func (n *Network) EnsureNodeConfig(node *Node) error {
	// Ensure the node has access to network configuration
	node.network = n

	if err := node.EnsureKeys(); err != nil {
		return stacktrace.Wrap(err)
	}

	// Ensure a data directory if not already set
	if len(node.DataDir) == 0 {
		node.DataDir = filepath.Join(n.Dir, node.NodeID.String())
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
		if slices.Contains(subnet.ValidatorIDs, nodeID) {
			subnetIDs = append(subnetIDs, subnet.SubnetID.String())
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
func (n *Network) CreateSubnets(ctx context.Context, log logging.Logger, apiNode *Node, restartRequired bool) error {
	createdSubnets := make([]*Subnet, 0, len(n.Subnets))
	for _, subnet := range n.Subnets {
		if len(subnet.ValidatorIDs) == 0 {
			return stacktrace.Errorf("subnet %s needs at least one validator", subnet.SubnetID)
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
				return stacktrace.Errorf("no pre-funded keys available to create subnet %q", subnet.Name)
			}
			subnet.OwningKey = n.PreFundedKeys[len(n.PreFundedKeys)-1]
			n.PreFundedKeys = n.PreFundedKeys[:len(n.PreFundedKeys)-1]
		}

		// Create the subnet on the network
		if err := subnet.Create(ctx, apiNode.GetAccessibleURI()); err != nil {
			return stacktrace.Wrap(err)
		}

		log.Info("created subnet",
			zap.String("name", subnet.Name),
			zap.Stringer("id", subnet.SubnetID),
		)

		// Persist the subnet configuration
		if err := subnet.Write(n.GetSubnetDir()); err != nil {
			return stacktrace.Wrap(err)
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
		return stacktrace.Wrap(err)
	}

	reconfiguredNodes := []*Node{}
	for _, node := range n.Nodes {
		existingTrackedSubnets := node.Flags[config.TrackSubnetsKey]
		trackedSubnets := n.TrackedSubnetsForNode(node.NodeID)
		if existingTrackedSubnets == trackedSubnets {
			continue
		}
		node.Flags[config.TrackSubnetsKey] = trackedSubnets
		reconfiguredNodes = append(reconfiguredNodes, node)
	}
	// http://127.0.0.1:62815
	if restartRequired {
		log.Info("restarting node(s) to enable them to track the new subnet(s)")

		runningNodes := make([]*Node, 0, len(reconfiguredNodes))
		for _, node := range reconfiguredNodes {
			if node.IsRunning() {
				runningNodes = append(runningNodes, node)
			}
		}

		if err := restartNodes(ctx, runningNodes); err != nil {
			return stacktrace.Wrap(err)
		}

		if err := WaitForHealthyNodes(ctx, n.log, runningNodes); err != nil {
			return stacktrace.Wrap(err)
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

		if err := subnet.AddValidators(ctx, log, apiNode.GetAccessibleURI(), validatorNodes...); err != nil {
			return stacktrace.Wrap(err)
		}
	}

	log.Info("finished adding validators for new subnet(s)")

	// Wait for nodes to become subnet validators
	pChainClient := platformvm.NewClient(apiNode.GetAccessibleURI())
	validatorsToRestart := set.Set[ids.NodeID]{}
	for _, subnet := range createdSubnets {
		if err := WaitForActiveValidators(ctx, log, pChainClient, subnet); err != nil {
			return stacktrace.Wrap(err)
		}

		// It should now be safe to create chains for the subnet
		if err := subnet.CreateChains(ctx, log, apiNode.GetAccessibleURI()); err != nil {
			return stacktrace.Wrap(err)
		}

		if err := subnet.Write(n.GetSubnetDir()); err != nil {
			return fmt.Errorf("failed to write subnet configuration for %q: %w", subnet.Name, err)
		}
		log.Info("wrote subnet configuration",
			zap.String("name", subnet.Name),
			zap.Stringer("id", subnet.SubnetID),
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

	if err := restartNodes(ctx, nodesToRestart); err != nil {
		return stacktrace.Wrap(err)
	}

	if err := WaitForHealthyNodes(ctx, log, nodesToRestart); err != nil {
		return stacktrace.Wrap(err)
	}

	return nil
}

func (n *Network) GetNode(nodeID ids.NodeID) (*Node, error) {
	for _, node := range n.Nodes {
		if node.NodeID == nodeID {
			return node, nil
		}
	}
	return nil, stacktrace.Errorf("%s is not known to the network", nodeID)
}

// GetNodeURIs returns the accessible URIs of nodes in the network that are running and not ephemeral.
func (n *Network) GetNodeURIs() []NodeURI {
	return GetNodeURIs(n.Nodes)
}

// GetAvailableNodeIDs returns the node IDs of nodes in the network that are running and not ephemeral.
func (n *Network) GetAvailableNodeIDs() []string {
	availableNodes := FilterAvailableNodes(n.Nodes)
	ids := []string{}
	for _, node := range availableNodes {
		ids = append(ids, node.NodeID.String())
	}
	return ids
}

// Retrieves bootstrap IPs and IDs for all non-ephemeral nodes except the skipped one
// (this supports collecting the bootstrap details for restarting a node).
//
// For consumption outside of avalanchego. Needs to be kept exported.
func (n *Network) GetBootstrapIPsAndIDs(skippedNode *Node) ([]string, []string) {
	bootstrapIPs := []string{}
	bootstrapIDs := []string{}
	for _, node := range n.Nodes {
		if node.IsEphemeral {
			// Ephemeral nodes are not guaranteed to stay running
			continue
		}
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

	return bootstrapIPs, bootstrapIDs
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

// GetGenesisFileContent returns the base64-encoded JSON-marshaled
// network genesis.
func (n *Network) GetGenesisFileContent() (string, error) {
	bytes, err := json.Marshal(n.Genesis)
	if err != nil {
		return "", stacktrace.Errorf("failed to marshal genesis: %w", err)
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// GetSubnetConfigContent returns the base64-encoded and
// JSON-marshaled map of subnetID to subnet configuration.
func (n *Network) GetSubnetConfigContent() (string, error) {
	subnetConfigs := map[ids.ID]ConfigMap{}

	if len(n.PrimarySubnetConfig) > 0 {
		subnetConfigs[constants.PrimaryNetworkID] = n.PrimarySubnetConfig
	}

	// Collect configuration for non-primary subnets
	for _, subnet := range n.Subnets {
		if subnet.SubnetID == ids.Empty {
			// The subnet hasn't been created yet and it's not
			// possible to supply configuration without an ID.
			continue
		}
		if len(subnet.Config) == 0 {
			continue
		}
		subnetConfigs[subnet.SubnetID] = subnet.Config
	}

	if len(subnetConfigs) == 0 {
		return "", nil
	}

	marshaledConfigs, err := json.Marshal(subnetConfigs)
	if err != nil {
		return "", stacktrace.Errorf("failed to marshal subnet configs: %w", err)
	}
	return base64.StdEncoding.EncodeToString(marshaledConfigs), nil
}

// GetChainConfigContent returns the base64-encoded and JSON-marshaled map of chain alias/ID
// to JSON-marshaled chain configuration for both primary and custom chains.
func (n *Network) GetChainConfigContent() (string, error) {
	chainConfigs := map[string]chains.ChainConfig{}
	for alias, flags := range n.PrimaryChainConfigs {
		marshaledFlags, err := json.Marshal(flags)
		if err != nil {
			return "", stacktrace.Errorf("failed to marshal flags map for %s-Chain: %w", alias, err)
		}
		chainConfigs[alias] = chains.ChainConfig{
			Config: marshaledFlags,
		}
	}

	// Collect custom chain configuration
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

	marshaledConfigs, err := json.Marshal(chainConfigs)
	if err != nil {
		return "", stacktrace.Errorf("failed to marshal chain configs: %w", err)
	}
	return base64.StdEncoding.EncodeToString(marshaledConfigs), nil
}

// GetMonitoringLabels retrieves the map of labels and their values to be
// applied to metrics and logs collected from nodes and other collection
// targets for the network (including test workloads). Callers may need
// to set a unique value for the `instance` label to ensure a stable
// identity for the collection target.
func (n *Network) GetMonitoringLabels() map[string]string {
	labels := map[string]string{
		"network_uuid": n.UUID,
		// This label must be set for compatibility with the expected
		// filtering. Nodes should override this value.
		"is_ephemeral_node": "false",
		"network_owner":     n.Owner,
	}
	maps.Copy(labels, GetGitHubLabels())
	return labels
}

// GetGitHubLabels returns a map of GitHub labels and their values if available.
func GetGitHubLabels() map[string]string {
	labels := map[string]string{}
	for _, label := range githubLabels {
		value := os.Getenv(strings.ToUpper(label))
		if len(value) > 0 {
			labels[label] = value
		}
	}
	return labels
}

// Waits until the provided nodes are healthy.
func waitForHealthy(ctx context.Context, log logging.Logger, nodes []*Node) error {
	ticker := time.NewTicker(networkHealthCheckInterval)
	defer ticker.Stop()

	unhealthyNodes := set.Of(nodes...)
	for {
		for node := range unhealthyNodes {
			healthy, err := node.IsHealthy(ctx)
			if err != nil {
				return stacktrace.Wrap(err)
			}
			if !healthy {
				continue
			}

			unhealthyNodes.Remove(node)
			log.Info("node is healthy",
				zap.Stringer("nodeID", node.NodeID),
				zap.String("uri", node.GetAccessibleURI()),
			)
		}

		if unhealthyNodes.Len() == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return stacktrace.Errorf("failed to see all nodes healthy before timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Retrieves the root dir for tmpnet data.
func getTmpnetPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Wrap(err)
	}
	return filepath.Join(homeDir, ".tmpnet"), nil
}

// Retrieves the default root dir for storing networks and their
// configuration.
func getDefaultRootNetworkDir() (string, error) {
	tmpnetPath, err := getTmpnetPath()
	if err != nil {
		return "", stacktrace.Wrap(err)
	}
	return filepath.Join(tmpnetPath, "networks"), nil
}

// Retrieves the path to a reusable network path for the given owner.
func GetReusableNetworkPathForOwner(owner string) (string, error) {
	networkPath, err := getDefaultRootNetworkDir()
	if err != nil {
		return "", stacktrace.Wrap(err)
	}
	return filepath.Join(networkPath, "latest_"+owner), nil
}

const invalidRPCVersion = 0

// checkVMBinaries checks that VM binaries for the given subnets exist and optionally checks that VM
// binaries have the same rpcchainvm version as the indicated avalanchego binary.
func checkVMBinaries(log logging.Logger, subnets []*Subnet, config *ProcessRuntimeConfig) error {
	if len(subnets) == 0 {
		// Without subnets there are no VM binaries to check
		return nil
	}

	if config == nil {
		log.Info("skipping rpcchainvm version check because the process runtime is not configured")
		return nil
	}

	avalanchegoRPCVersion, err := getRPCVersion(log, config.AvalancheGoPath, "--version-json")
	if err != nil {
		log.Warn("unable to check rpcchainvm version for avalanchego", zap.Error(err))
		return nil
	}

	var incompatibleChains bool
	for _, subnet := range subnets {
		for _, chain := range subnet.Chains {
			vmPath := filepath.Join(config.PluginDir, chain.VMID.String())

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
			vmRPCVersion, err := getRPCVersion(log, vmPath, chain.VersionArgs...)
			if err != nil {
				log.Warn("unable to check rpcchainvm version for VM Binary",
					zap.String("subnet", subnet.Name),
					zap.Error(err),
				)
			} else if avalanchegoRPCVersion != vmRPCVersion {
				log.Error("unexpected rpcchainvm version for VM binary",
					zap.String("subnet", subnet.Name),
					zap.String("avalanchegoPath", config.AvalancheGoPath),
					zap.Uint64("avalanchegoRPCVersion", avalanchegoRPCVersion),
					zap.String("vmPath", vmPath),
					zap.Uint64("vmRPCVersion", vmRPCVersion),
				)
				incompatibleChains = true
			}
		}
	}

	if incompatibleChains {
		return stacktrace.New("the rpcchainvm version of the VMs for one or more chains may not be compatible with the specified avalanchego binary")
	}
	return nil
}

type RPCChainVMVersion struct {
	RPCChainVM uint64 `json:"rpcchainvm"`
}

// getRPCVersion attempts to invoke the given command with the specified version arguments and
// retrieve an rpcchainvm version from its output.
func getRPCVersion(log logging.Logger, command string, versionArgs ...string) (uint64, error) {
	cmd := exec.Command(command, versionArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, stacktrace.Errorf("command %q failed with output: %s", command, output)
	}

	// Ignore output before the opening brace to tolerate the case of a command being invoked
	// with `go run` and the go toolchain emitting diagnostic logging before the version output.
	if idx := bytes.IndexByte(output, '{'); idx > 0 {
		log.Info("ignoring leading bytes of JSON version output in advance of opening `{`",
			zap.String("command", command),
			zap.String("ignoredLeadingBytes", string(output[:idx])),
		)
		output = output[idx:]
	}

	version := &RPCChainVMVersion{}
	if err := json.Unmarshal(output, version); err != nil {
		return 0, stacktrace.Errorf("failed to unmarshal output from command %q: %w, output: %s", command, err, output)
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
		"https://%s/d/kBQpRdWnk/avalanche-main-dashboard?&var-filter=network_uuid%%7C%%3D%%7C%s&var-filter=is_ephemeral_node%%7C%%3D%%7Cfalse&from=%s&to=%s",
		grafanaURI,
		networkUUID,
		startTime,
		endTime,
	)
}
