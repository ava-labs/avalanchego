// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

// The Node type is defined in this file (node.go - orchestration) and
// node_config.go (reading/writing configuration).

const (
	defaultNodeTickerInterval = 50 * time.Millisecond
)

var (
	errMissingTLSKeyForNodeID = fmt.Errorf("failed to ensure node ID: missing value for %q", config.StakingTLSKeyContentKey)
	errMissingCertForNodeID   = fmt.Errorf("failed to ensure node ID: missing value for %q", config.StakingCertContentKey)
	errInvalidKeypair         = fmt.Errorf("%q and %q must be provided together or not at all", config.StakingTLSKeyContentKey, config.StakingCertContentKey)
)

// NodeRuntime defines the methods required to support running a node.
type NodeRuntime interface {
	readState(ctx context.Context) error
	GetLocalURI(ctx context.Context) (string, func(), error)
	GetLocalStakingAddress(ctx context.Context) (netip.AddrPort, func(), error)
	Start(ctx context.Context) error
	InitiateStop(ctx context.Context) error
	WaitForStopped(ctx context.Context) error
	Restart(ctx context.Context) error
	IsHealthy(ctx context.Context) (bool, error)
}

// Configuration required to configure a node runtime. Only one of the fields should be set.
type NodeRuntimeConfig struct {
	Process *ProcessRuntimeConfig `json:"process,omitempty"`
	Kube    *KubeRuntimeConfig    `json:"kube,omitempty"`
}

// GetNetworkStartTimeout returns the timeout to use when starting a network.
func (c *NodeRuntimeConfig) GetNetworkStartTimeout(nodeCount int) (time.Duration, error) {
	switch {
	case c.Process != nil:
		// Processes are expected to start quickly, nodeCount is ignored
		return DefaultNetworkTimeout, nil
	case c.Kube != nil:
		// Ensure sufficient time for scheduling and image pull
		timeout := time.Duration(nodeCount) * time.Minute

		if c.Kube.UseExclusiveScheduling {
			// Ensure sufficient time for the creation of autoscaled nodes
			timeout *= 2
		}

		return timeout, nil
	default:
		return 0, errors.New("no runtime configuration set")
	}
}

// Node supports configuring and running a node participating in a temporary network.
type Node struct {
	// Set by EnsureNodeID which is also called when the node is read.
	NodeID ids.NodeID

	// The set of flags used to start whose values are intended to deviate from the
	// default set of flags configured for the network.
	Flags FlagsMap

	// An ephemeral node is not expected to be a persistent member of the network and
	// should therefore not be used as for bootstrapping purposes.
	IsEphemeral bool

	// Optional, the configuration used to initialize the node runtime.
	// If not set, the network default will be used.
	RuntimeConfig *NodeRuntimeConfig

	// Runtime state, intended to be set by NodeRuntime
	URI            string
	StakingAddress netip.AddrPort

	// Defaults to [network dir]/[node id] if not set
	DataDir string

	// Initialized on demand
	runtime NodeRuntime

	network *Network
}

// Initializes a new node with only the data dir set
func NewNode() *Node {
	return &Node{
		Flags: FlagsMap{},
	}
}

// Initializes an ephemeral node using the provided config flags
func NewEphemeralNode(flags FlagsMap) *Node {
	node := NewNode()
	node.Flags = flags
	node.IsEphemeral = true

	return node
}

// Initializes the specified number of nodes.
func NewNodesOrPanic(count int) []*Node {
	nodes := make([]*Node, count)
	for i := range nodes {
		node := NewNode()
		if err := node.EnsureKeys(); err != nil {
			panic(err)
		}
		nodes[i] = node
	}
	return nodes
}

// Retrieves the runtime for the node.
func (n *Node) getRuntime() NodeRuntime {
	if n.runtime == nil {
		switch {
		case n.getRuntimeConfig().Process != nil:
			n.runtime = &ProcessRuntime{
				node: n,
			}
		case n.getRuntimeConfig().Kube != nil:
			n.runtime = &KubeRuntime{
				node: n,
			}
		default:
			// Runtime configuration is validated during flag handling and network
			// bootstrap so misconfiguration should be unusual.
			panic(fmt.Sprintf("no runtime configuration set for %q", n.NodeID))
		}
	}
	return n.runtime
}

// Retrieves the runtime configuration for the node, defaulting to the
// runtime configuration from the network if none is set for the node.
func (n *Node) getRuntimeConfig() NodeRuntimeConfig {
	if n.RuntimeConfig != nil {
		return *n.RuntimeConfig
	}
	return n.network.DefaultRuntimeConfig
}

// Runtime methods

func (n *Node) IsHealthy(ctx context.Context) (bool, error) {
	return n.getRuntime().IsHealthy(ctx)
}

func (n *Node) Start(ctx context.Context) error {
	return n.getRuntime().Start(ctx)
}

func (n *Node) InitiateStop(ctx context.Context) error {
	if err := n.SaveMetricsSnapshot(ctx); err != nil {
		return err
	}
	return n.getRuntime().InitiateStop(ctx)
}

func (n *Node) WaitForStopped(ctx context.Context) error {
	return n.getRuntime().WaitForStopped(ctx)
}

func (n *Node) Restart(ctx context.Context) error {
	// Ensure the config used to restart the node is persisted for future use
	if err := n.Write(); err != nil {
		return err
	}
	return n.getRuntime().Restart(ctx)
}

func (n *Node) readState(ctx context.Context) error {
	return n.getRuntime().readState(ctx)
}

func (n *Node) GetLocalURI(ctx context.Context) (string, func(), error) {
	return n.getRuntime().GetLocalURI(ctx)
}

func (n *Node) GetLocalStakingAddress(ctx context.Context) (netip.AddrPort, func(), error) {
	return n.getRuntime().GetLocalStakingAddress(ctx)
}

// Writes the current state of the metrics endpoint to disk
func (n *Node) SaveMetricsSnapshot(ctx context.Context) error {
	if len(n.URI) == 0 {
		// No URI to request metrics from
		return nil
	}
	baseURI, cancel, err := n.GetLocalURI(ctx)
	if err != nil {
		return nil
	}
	defer cancel()
	uri := baseURI + "/ext/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return n.writeMetricsSnapshot(body)
}

// Initiates node shutdown and waits for the node to stop.
func (n *Node) Stop(ctx context.Context) error {
	if err := n.InitiateStop(ctx); err != nil {
		return err
	}
	return n.WaitForStopped(ctx)
}

// Ensures staking and signing keys are generated if not already present and
// that the node ID (derived from the staking keypair) is set.
func (n *Node) EnsureKeys() error {
	if err := n.EnsureBLSSigningKey(); err != nil {
		return err
	}
	if err := n.EnsureStakingKeypair(); err != nil {
		return err
	}
	return n.EnsureNodeID()
}

// Ensures a BLS signing key is generated if not already present.
func (n *Node) EnsureBLSSigningKey() error {
	// Attempt to retrieve an existing key
	existingKey := n.Flags[config.StakingSignerKeyContentKey]
	if len(existingKey) > 0 {
		// Nothing to do
		return nil
	}

	// Generate a new signing key
	newKey, err := localsigner.New()
	if err != nil {
		return fmt.Errorf("failed to generate staking signer key: %w", err)
	}
	n.Flags[config.StakingSignerKeyContentKey] = base64.StdEncoding.EncodeToString(newKey.ToBytes())
	return nil
}

// Ensures a staking keypair is generated if not already present.
func (n *Node) EnsureStakingKeypair() error {
	keyKey := config.StakingTLSKeyContentKey
	certKey := config.StakingCertContentKey

	key := n.Flags[keyKey]
	cert := n.Flags[certKey]
	if len(key) == 0 && len(cert) == 0 {
		// Generate new keypair
		tlsCertBytes, tlsKeyBytes, err := staking.NewCertAndKeyBytes()
		if err != nil {
			return fmt.Errorf("failed to generate staking keypair: %w", err)
		}
		n.Flags[keyKey] = base64.StdEncoding.EncodeToString(tlsKeyBytes)
		n.Flags[certKey] = base64.StdEncoding.EncodeToString(tlsCertBytes)
	} else if len(key) == 0 || len(cert) == 0 {
		// Only one of key and cert was provided
		return errInvalidKeypair
	}

	return nil
}

// Derives the nodes proof-of-possession. Requires the node to have a
// BLS signing key.
func (n *Node) GetProofOfPossession() (*signer.ProofOfPossession, error) {
	signingKey := n.Flags[config.StakingSignerKeyContentKey]
	signingKeyBytes, err := base64.StdEncoding.DecodeString(signingKey)
	if err != nil {
		return nil, err
	}
	secretKey, err := localsigner.FromBytes(signingKeyBytes)
	if err != nil {
		return nil, err
	}
	pop, err := signer.NewProofOfPossession(secretKey)
	if err != nil {
		return nil, err
	}

	return pop, nil
}

// Derives the node ID. Requires that a tls keypair is present.
func (n *Node) EnsureNodeID() error {
	keyKey := config.StakingTLSKeyContentKey
	certKey := config.StakingCertContentKey

	key := n.Flags[keyKey]
	if len(key) == 0 {
		return errMissingTLSKeyForNodeID
	}
	keyBytes, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("failed to ensure node ID: failed to base64 decode value for %q: %w", keyKey, err)
	}

	cert := n.Flags[certKey]
	if len(cert) == 0 {
		return errMissingCertForNodeID
	}
	certBytes, err := base64.StdEncoding.DecodeString(cert)
	if err != nil {
		return fmt.Errorf("failed to ensure node ID: failed to base64 decode value for %q: %w", certKey, err)
	}

	tlsCert, err := staking.LoadTLSCertFromBytes(keyBytes, certBytes)
	if err != nil {
		return fmt.Errorf("failed to ensure node ID: failed to load tls cert: %w", err)
	}
	stakingCert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	if err != nil {
		return fmt.Errorf("failed to ensure node ID: failed to parse staking cert: %w", err)
	}
	n.NodeID = ids.NodeIDFromCert(stakingCert)

	return nil
}

// GetUniqueID returns a globally unique identifier for the node.
func (n *Node) GetUniqueID() string {
	nodeIDString := n.NodeID.String()
	startIndex := len(ids.NodeIDPrefix)
	endIndex := startIndex + 8 // 8 characters should be enough to identify a node in the context of its network
	return n.network.UUID + "-" + strings.ToLower(nodeIDString[startIndex:endIndex])
}

// composeFlags determines the set of flags that should be used to
// start the node.
func (n *Node) composeFlags() (FlagsMap, error) {
	flags := maps.Clone(n.Flags)

	// Apply the network defaults first so that they are not overridden
	flags.SetDefaults(n.network.DefaultFlags)

	flags.SetDefaults(DefaultTmpnetFlags())

	// Convert the network id to a string to ensure consistency in JSON round-tripping.
	flags.SetDefault(config.NetworkNameKey, strconv.FormatUint(uint64(n.network.GetNetworkID()), 10))

	// Set the bootstrap configuration
	bootstrapIPs, bootstrapIDs := n.network.GetBootstrapIPsAndIDs(n)
	flags.SetDefault(config.BootstrapIDsKey, strings.Join(bootstrapIDs, ","))
	flags.SetDefault(config.BootstrapIPsKey, strings.Join(bootstrapIPs, ","))

	// TODO(marun) Maybe avoid computing content flags for each node start?

	if n.network.Genesis != nil {
		genesisFileContent, err := n.network.GetGenesisFileContent()
		if err != nil {
			return nil, fmt.Errorf("failed to get genesis file content: %w", err)
		}
		flags.SetDefault(config.GenesisFileContentKey, genesisFileContent)

		isSingleNodeNetwork := len(n.network.Nodes) == 1 && len(n.network.Genesis.InitialStakers) == 1
		if isSingleNodeNetwork {
			n.network.log.Info("defaulting to sybil protection disabled to enable a single-node network to start")
			flags.SetDefault(config.SybilProtectionEnabledKey, "false")
		}
	}

	subnetConfigContent, err := n.network.GetSubnetConfigContent()
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet config content: %w", err)
	}
	if len(subnetConfigContent) > 0 {
		flags.SetDefault(config.SubnetConfigContentKey, subnetConfigContent)
	}

	chainConfigContent, err := n.network.GetChainConfigContent()
	if err != nil {
		return nil, fmt.Errorf("failed to get chain config content: %w", err)
	}
	if len(chainConfigContent) > 0 {
		flags.SetDefault(config.ChainConfigContentKey, chainConfigContent)
	}

	return flags, nil
}

// WaitForHealthy blocks until node health is true or an error (including context timeout) is observed.
func (n *Node) WaitForHealthy(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("unable to wait for health for node %q with a context without a deadline", n.NodeID)
	}
	ticker := time.NewTicker(DefaultNodeTickerInterval)
	defer ticker.Stop()

	for {
		healthy, err := n.IsHealthy(ctx)
		switch {
		case errors.Is(err, ErrUnrecoverableNodeHealthCheck):
			return fmt.Errorf("%w for node %q", err, n.NodeID)
		case err != nil:
			n.network.log.Verbo("failed to query node health",
				zap.Stringer("nodeID", n.NodeID),
				zap.Error(err),
			)
			continue
		case healthy:
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to wait for health of node %q before timeout: %w", n.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// getMonitoringLabels retrieves the map of labels and their values to be
// applied to metrics and logs collected from the node.
func (n *Node) getMonitoringLabels() map[string]string {
	labels := n.network.GetMonitoringLabels()

	// Explicitly setting an instance label avoids the default
	// behavior of using the node's URI since the URI isn't
	// guaranteed stable (e.g. port may change after restart).
	labels["instance"] = n.GetUniqueID()

	labels["node_id"] = n.NodeID.String()
	labels["is_ephemeral_node"] = strconv.FormatBool(n.IsEphemeral)

	return labels
}

func (n *Node) IsRunning() bool {
	return len(n.URI) > 0
}
