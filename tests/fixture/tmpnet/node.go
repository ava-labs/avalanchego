// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
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
	readState() error
	GetLocalURI(ctx context.Context) (string, func(), error)
	GetLocalStakingAddress(ctx context.Context) (netip.AddrPort, func(), error)
	Start(log logging.Logger) error
	InitiateStop() error
	WaitForStopped(ctx context.Context) error
	IsHealthy(ctx context.Context) (bool, error)
}

// Configuration required to configure a node runtime.
type NodeRuntimeConfig struct {
	Process *ProcessRuntimeConfig
}

type ProcessRuntimeConfig struct {
	AvalancheGoPath   string
	PluginDir         string
	ReuseDynamicPorts bool
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

	// Initialized on demand
	runtime NodeRuntime

	// Intended to be set by the network
	network *Network
}

// Initializes a new node with only the data dir set
func NewNode(dataDir string) *Node {
	return &Node{
		Flags: FlagsMap{
			config.DataDirKey: dataDir,
		},
	}
}

// Initializes an ephemeral node using the provided config flags
func NewEphemeralNode(flags FlagsMap) *Node {
	node := NewNode("")
	node.Flags = flags
	node.IsEphemeral = true

	return node
}

// Initializes the specified number of nodes.
func NewNodesOrPanic(count int) []*Node {
	nodes := make([]*Node, count)
	for i := range nodes {
		node := NewNode("")
		if err := node.EnsureKeys(); err != nil {
			panic(err)
		}
		nodes[i] = node
	}
	return nodes
}

// Reads a node's configuration from the specified directory.
func ReadNode(dataDir string) (*Node, error) {
	node := NewNode(dataDir)
	return node, node.Read()
}

// Reads nodes from the specified network directory.
func ReadNodes(network *Network, includeEphemeral bool) ([]*Node, error) {
	nodes := []*Node{}

	// Node configuration is stored in child directories
	entries, err := os.ReadDir(network.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read dir: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		nodeDir := filepath.Join(network.Dir, entry.Name())
		node, err := ReadNode(nodeDir)
		if errors.Is(err, os.ErrNotExist) {
			// If no config file exists, assume this is not the path of a node
			continue
		} else if err != nil {
			return nil, err
		}

		if !includeEphemeral && node.IsEphemeral {
			continue
		}

		if err := node.EnsureNodeID(); err != nil {
			return nil, fmt.Errorf("failed to ensure NodeID: %w", err)
		}

		node.network = network

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// Retrieves the runtime for the node.
func (n *Node) getRuntime() NodeRuntime {
	if n.runtime == nil {
		n.runtime = &NodeProcess{
			node: n,
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

func (n *Node) Start(log logging.Logger) error {
	return n.getRuntime().Start(log)
}

func (n *Node) InitiateStop(ctx context.Context) error {
	if err := n.SaveMetricsSnapshot(ctx); err != nil {
		return err
	}
	return n.getRuntime().InitiateStop()
}

func (n *Node) WaitForStopped(ctx context.Context) error {
	return n.getRuntime().WaitForStopped(ctx)
}

func (n *Node) readState() error {
	return n.getRuntime().readState()
}

func (n *Node) GetDataDir() string {
	return cast.ToString(n.Flags[config.DataDirKey])
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
	existingKey, err := n.Flags.GetStringVal(config.StakingSignerKeyContentKey)
	if err != nil {
		return err
	}
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

	key, err := n.Flags.GetStringVal(keyKey)
	if err != nil {
		return err
	}

	cert, err := n.Flags.GetStringVal(certKey)
	if err != nil {
		return err
	}

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
	signingKey, err := n.Flags.GetStringVal(config.StakingSignerKeyContentKey)
	if err != nil {
		return nil, err
	}
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

	key, err := n.Flags.GetStringVal(keyKey)
	if err != nil {
		return err
	}
	if len(key) == 0 {
		return errMissingTLSKeyForNodeID
	}
	keyBytes, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("failed to ensure node ID: failed to base64 decode value for %q: %w", keyKey, err)
	}

	cert, err := n.Flags.GetStringVal(certKey)
	if err != nil {
		return err
	}
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

// Saves the currently allocated API port to the node's configuration
// for use across restarts.
func (n *Node) SaveAPIPort() error {
	hostPort := strings.TrimPrefix(n.URI, "http://")
	if len(hostPort) == 0 {
		// Without an API URI there is nothing to save
		return nil
	}
	_, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return err
	}
	n.Flags[config.HTTPPortKey] = port
	return nil
}
