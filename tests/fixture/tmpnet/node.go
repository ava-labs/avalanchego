// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

var (
	errNodeAlreadyRunning     = errors.New("failed to start node: node is already running")
	errMissingTLSKeyForNodeID = fmt.Errorf("failed to ensure node ID: missing value for %q", config.StakingTLSKeyContentKey)
	errMissingCertForNodeID   = fmt.Errorf("failed to ensure node ID: missing value for %q", config.StakingCertContentKey)
	errInvalidKeypair         = fmt.Errorf("%q and %q must be provided together or not at all", config.StakingTLSKeyContentKey, config.StakingCertContentKey)
)

// Defines configuration to execute a node.
//
// TODO(marun) Support persisting this configuration per-node when
// node restart is implemented. Currently it can be supplied for node
// start but won't survive restart.
type NodeRuntimeConfig struct {
	// Path to avalanchego binary
	ExecPath string
}

// Stores the configuration and process details of a node in a temporary network.
type Node struct {
	NodeRuntimeConfig
	node.NodeProcessContext

	NodeID ids.NodeID
	Flags  FlagsMap

	// Configuration is intended to be stored at the path identified in NodeConfig.Flags[config.DataDirKey]
}

func NewNode(dataDir string) *Node {
	return &Node{
		Flags: FlagsMap{
			config.DataDirKey: dataDir,
		},
	}
}

// Attempt to read configuration and process details for a node
// from the specified directory.
func ReadNode(dataDir string) (*Node, error) {
	node := NewNode(dataDir)
	if _, err := os.Stat(node.GetConfigPath()); err != nil {
		return nil, fmt.Errorf("failed to read node config file: %w", err)
	}
	return node, node.ReadAll()
}

func (n *Node) GetDataDir() string {
	return cast.ToString(n.Flags[config.DataDirKey])
}

func (n *Node) GetConfigPath() string {
	return filepath.Join(n.GetDataDir(), "config.json")
}

func (n *Node) ReadConfig() error {
	bytes, err := os.ReadFile(n.GetConfigPath())
	if err != nil {
		return fmt.Errorf("failed to read node config: %w", err)
	}
	flags := FlagsMap{}
	if err := json.Unmarshal(bytes, &flags); err != nil {
		return fmt.Errorf("failed to unmarshal node config: %w", err)
	}
	n.Flags = flags
	if err := n.EnsureNodeID(); err != nil {
		return err
	}
	return nil
}

func (n *Node) WriteConfig() error {
	if err := os.MkdirAll(n.GetDataDir(), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create node dir: %w", err)
	}

	bytes, err := DefaultJSONMarshal(n.Flags)
	if err != nil {
		return fmt.Errorf("failed to marshal node config: %w", err)
	}

	if err := os.WriteFile(n.GetConfigPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write node config: %w", err)
	}
	return nil
}

func (n *Node) GetProcessContextPath() string {
	return filepath.Join(n.GetDataDir(), config.DefaultProcessContextFilename)
}

func (n *Node) ReadProcessContext() error {
	path := n.GetProcessContextPath()
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		// The absence of the process context file indicates the node is not running
		n.NodeProcessContext = node.NodeProcessContext{}
		return nil
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read node process context: %w", err)
	}
	processContext := node.NodeProcessContext{}
	if err := json.Unmarshal(bytes, &processContext); err != nil {
		return fmt.Errorf("failed to unmarshal node process context: %w", err)
	}
	n.NodeProcessContext = processContext
	return nil
}

func (n *Node) ReadAll() error {
	if err := n.ReadConfig(); err != nil {
		return err
	}
	return n.ReadProcessContext()
}

func (n *Node) Start(w io.Writer, defaultExecPath string) error {
	// Avoid attempting to start an already running node.
	proc, err := n.GetProcess()
	if err != nil {
		return fmt.Errorf("failed to start node: %w", err)
	}
	if proc != nil {
		return errNodeAlreadyRunning
	}

	// Ensure a stale process context file is removed so that the
	// creation of a new file can indicate node start.
	if err := os.Remove(n.GetProcessContextPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove stale process context file: %w", err)
	}

	execPath := n.ExecPath
	if len(execPath) == 0 {
		execPath = defaultExecPath
	}

	cmd := exec.Command(execPath, "--config-file", n.GetConfigPath())
	if err := cmd.Start(); err != nil {
		return err
	}

	// Determine appropriate level of node description detail
	nodeDescription := fmt.Sprintf("node %q", n.NodeID)
	isEphemeralNode := filepath.Base(filepath.Dir(n.GetDataDir())) == defaultEphemeralDirName
	if isEphemeralNode {
		nodeDescription = "ephemeral " + nodeDescription
	}
	nonDefaultNodeDir := filepath.Base(n.GetDataDir()) != n.NodeID.String()
	if nonDefaultNodeDir {
		// Only include the data dir if its base is not the default (the node ID)
		nodeDescription = fmt.Sprintf("%s with path: %s", nodeDescription, n.GetDataDir())
	}

	go func() {
		if err := cmd.Wait(); err != nil {
			if err.Error() != "signal: killed" {
				_, _ = fmt.Fprintf(w, "%s finished with error: %v\n", nodeDescription, err)
			}
		}
		_, _ = fmt.Fprintf(w, "%s exited\n", nodeDescription)
	}()

	// A node writes a process context file on start. If the file is not
	// found in a reasonable amount of time, the node is unlikely to have
	// started successfully.
	if err := n.WaitForProcessContext(context.Background()); err != nil {
		return fmt.Errorf("failed to start node: %w", err)
	}

	_, err = fmt.Fprintf(w, "Started %s\n", nodeDescription)
	return err
}

// Retrieve the node process if it is running. As part of determining
// process liveness, the node's process context will be refreshed if
// live or cleared if not running.
func (n *Node) GetProcess() (*os.Process, error) {
	// Read the process context to ensure freshness. The node may have
	// stopped or been restarted since last read.
	if err := n.ReadProcessContext(); err != nil {
		return nil, fmt.Errorf("failed to read process context: %w", err)
	}

	if n.PID == 0 {
		// Process is not running
		return nil, nil
	}

	proc, err := os.FindProcess(n.PID)
	if err != nil {
		return nil, fmt.Errorf("failed to find process: %w", err)
	}

	// Sending 0 will not actually send a signal but will perform
	// error checking.
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		// Process is running
		return proc, nil
	}
	if errors.Is(err, os.ErrProcessDone) {
		// Process is not running
		return nil, nil
	}
	return nil, fmt.Errorf("failed to determine process status: %w", err)
}

// Signals the node process to stop and waits for the node process to
// stop running.
func (n *Node) Stop() error {
	proc, err := n.GetProcess()
	if err != nil {
		return fmt.Errorf("failed to retrieve process to stop: %w", err)
	}
	if proc == nil {
		// Already stopped
		return nil
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to pid %d: %w", n.PID, err)
	}

	// Wait for the node process to stop
	ticker := time.NewTicker(DefaultNodeTickerInterval)
	defer ticker.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), DefaultNodeStopTimeout)
	defer cancel()
	for {
		proc, err := n.GetProcess()
		if err != nil {
			return fmt.Errorf("failed to retrieve process: %w", err)
		}
		if proc == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see node process stop %q before timeout: %w", n.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (n *Node) IsHealthy(ctx context.Context) (bool, error) {
	// Check that the node process is running as a precondition for
	// checking health. GetProcess will also ensure that the node's
	// API URI is current.
	proc, err := n.GetProcess()
	if err != nil {
		return false, fmt.Errorf("failed to determine process status: %w", err)
	}
	if proc == nil {
		return false, ErrNotRunning
	}

	// Check that the node is reporting healthy
	health, err := health.NewClient(n.URI).Health(ctx, nil)
	if err == nil {
		return health.Healthy, nil
	}

	switch t := err.(type) {
	case *net.OpError:
		if t.Op == "read" {
			// Connection refused - potentially recoverable
			return false, nil
		}
	case syscall.Errno:
		if t == syscall.ECONNREFUSED {
			// Connection refused - potentially recoverable
			return false, nil
		}
	}
	// Assume all other errors are not recoverable
	return false, fmt.Errorf("failed to query node health: %w", err)
}

func (n *Node) WaitForProcessContext(ctx context.Context) error {
	ticker := time.NewTicker(DefaultNodeTickerInterval)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, DefaultNodeInitTimeout)
	defer cancel()
	for len(n.URI) == 0 {
		err := n.ReadProcessContext()
		if err != nil {
			return fmt.Errorf("failed to read process context for node %q: %w", n.NodeID, err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to load process context for node %q before timeout: %w", n.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

// Convenience method for setting networking flags.
func (n *Node) SetNetworkingConfig(bootstrapIDs []string, bootstrapIPs []string) {
	var (
		// Use dynamic port allocation.
		httpPort    uint16 = 0
		stakingPort uint16 = 0
	)
	n.Flags[config.HTTPPortKey] = httpPort
	n.Flags[config.StakingPortKey] = stakingPort
	n.Flags[config.BootstrapIDsKey] = strings.Join(bootstrapIDs, ",")
	n.Flags[config.BootstrapIPsKey] = strings.Join(bootstrapIPs, ",")
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
	// Once a staking keypair is guaranteed it is safe to derive the node ID
	return n.EnsureNodeID()
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
	secretKey, err := bls.SecretKeyFromBytes(signingKeyBytes)
	if err != nil {
		return nil, err
	}
	return signer.NewProofOfPossession(secretKey), nil
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
	newKey, err := bls.NewSecretKey()
	if err != nil {
		return fmt.Errorf("failed to generate staking signer key: %w", err)
	}
	n.Flags[config.StakingSignerKeyContentKey] = base64.StdEncoding.EncodeToString(bls.SerializeSecretKey(newKey))
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

	err = n.EnsureNodeID()
	if err != nil {
		return fmt.Errorf("failed to derive a node ID: %w", err)
	}

	return nil
}

// Attempt to derive the node ID from the node configuration.
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
	stakingCert := staking.CertificateFromX509(tlsCert.Leaf)
	n.NodeID = ids.NodeIDFromCert(stakingCert)

	return nil
}
