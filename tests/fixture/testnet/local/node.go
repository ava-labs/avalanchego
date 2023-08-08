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
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/perms"
)

var (
	errProcessNotRunning  = errors.New("process not running")
	errNodeAlreadyRunning = errors.New("failed to start local node: node is already running")
)

// Defines local-specific node configuration. Supports setting default
// and node-specific values.
//
// TODO(marun) Support persisting this configuration per-node when
// node restart is implemented. Currently it can be supplied for node
// start but won't survive restart.
type LocalConfig struct {
	// Path to avalanchego binary
	ExecPath string
}

// Stores the configuration and process details of a node in a local network.
type LocalNode struct {
	testnet.NodeConfig
	LocalConfig
	node.NodeProcessContext

	// Configuration is intended to be stored at the path identified in NodeConfig.Flags[config.DataDirKey]
}

func NewLocalNode(dataDir string) *LocalNode {
	return &LocalNode{
		NodeConfig: testnet.NodeConfig{
			Flags: testnet.FlagsMap{
				config.DataDirKey: dataDir,
			},
		},
	}
}

// Attempt to read configuration and process details for a local node
// from the specified directory.
func ReadNode(dataDir string) (*LocalNode, error) {
	node := NewLocalNode(dataDir)
	if _, err := os.Stat(node.GetConfigPath()); err != nil {
		return nil, fmt.Errorf("failed to read local node config file: %w", err)
	}
	return node, node.ReadAll()
}

// Retrieve the ID of the node. The returned value may be nil if the
// node configuration has not yet been populated or read.
func (n *LocalNode) GetID() ids.NodeID {
	return n.NodeConfig.NodeID
}

// Retrieve backend-agnostic node configuration.
func (n *LocalNode) GetConfig() testnet.NodeConfig {
	return n.NodeConfig
}

// Retrieve backend-agnostic process details.
func (n *LocalNode) GetProcessContext() node.NodeProcessContext {
	return n.NodeProcessContext
}

func (n *LocalNode) GetDataDir() string {
	return cast.ToString(n.Flags[config.DataDirKey])
}

func (n *LocalNode) GetConfigPath() string {
	return filepath.Join(n.GetDataDir(), "config.json")
}

func (n *LocalNode) ReadConfig() error {
	bytes, err := os.ReadFile(n.GetConfigPath())
	if err != nil {
		return fmt.Errorf("failed to read local node config: %w", err)
	}
	flags := testnet.FlagsMap{}
	if err := json.Unmarshal(bytes, &flags); err != nil {
		return fmt.Errorf("failed to unmarshal local node config: %w", err)
	}
	config := testnet.NodeConfig{Flags: flags}
	if err := config.EnsureNodeID(); err != nil {
		return err
	}
	n.NodeConfig = config
	return nil
}

func (n *LocalNode) WriteConfig() error {
	if err := os.MkdirAll(n.GetDataDir(), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create node dir: %w", err)
	}

	bytes, err := testnet.DefaultJSONMarshal(n.Flags)
	if err != nil {
		return fmt.Errorf("failed to marshal local node config: %w", err)
	}

	if err := os.WriteFile(n.GetConfigPath(), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write local node config: %w", err)
	}
	return nil
}

func (n *LocalNode) GetProcessContextPath() string {
	return filepath.Join(n.GetDataDir(), config.ProcessContextFilename)
}

func (n *LocalNode) ReadProcessContext() error {
	path := n.GetProcessContextPath()
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		// The absence of the process context file indicates the node is not running
		n.NodeProcessContext = node.NodeProcessContext{}
		return nil
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read local node process context: %w", err)
	}
	processContext := node.NodeProcessContext{}
	if err := json.Unmarshal(bytes, &processContext); err != nil {
		return fmt.Errorf("failed to unmarshal local node process context: %w", err)
	}
	n.NodeProcessContext = processContext
	return nil
}

func (n *LocalNode) ReadAll() error {
	if err := n.ReadConfig(); err != nil {
		return err
	}
	return n.ReadProcessContext()
}

func (n *LocalNode) Start(w io.Writer, defaultExecPath string) error {
	// Avoid attempting to start an already running node.
	proc, err := n.GetProcess()
	if err != nil {
		return fmt.Errorf("failed to start local node: %w", err)
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

	go func() {
		if err := cmd.Wait(); err != nil {
			if err.Error() != "signal: killed" {
				_, _ = fmt.Fprintf(w, "node %q finished with error: %v\n", n.NodeID, err)
			}
		}
		_, _ = fmt.Fprintf(w, "node %q exited\n", n.NodeID)
	}()

	// A node writes a process context file on start. If the file is not
	// found in a reasonable amount of time, the node is unlikely to have
	// started successfully.
	if err := n.WaitForProcessContext(context.Background()); err != nil {
		return fmt.Errorf("failed to start local node: %w", err)
	}

	_, err = fmt.Fprintf(w, "Started %s\n", n.NodeID)
	return err
}

// Retrieve the node process if it is running. As part of determining
// process liveness, the node's process context will be refreshed if
// live or cleared if not running.
func (n *LocalNode) GetProcess() (*os.Process, error) {
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
func (n *LocalNode) Stop() error {
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

func (n *LocalNode) IsHealthy(ctx context.Context) (bool, error) {
	// Check that the node process is running as a precondition for
	// checking health. GetProcess will also ensure that the node's
	// API URI is current.
	proc, err := n.GetProcess()
	if err != nil {
		return false, fmt.Errorf("failed to determine process status: %w", err)
	}
	if proc == nil {
		return false, errProcessNotRunning
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
	return false, err
}

func (n *LocalNode) WaitForProcessContext(ctx context.Context) error {
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
