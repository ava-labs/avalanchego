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
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/api/health"
	cfg "github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/perms"
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
	// Whether a node is configured with static (e.g. 9560) or dynamic
	// (e.g. 0) tcp ports for both staking and api connections.
	UseStaticPorts bool
	// The port number to start from when assigning static ports.
	InitialStaticPort uint16
}

// Stores the configuration and process details of a node in a local network.
type LocalNode struct {
	testnet.NodeConfig
	LocalConfig
	node.NodeProcessContext

	// Configuration is intended to be stored at the path identified in NodeConfig.Flags[cfg.DataDirKey]
}

func NewLocalNode(dataDir string) *LocalNode {
	return &LocalNode{
		NodeConfig: testnet.NodeConfig{
			Flags: testnet.FlagsMap{
				cfg.DataDirKey: dataDir,
			},
		},
	}
}

// Attempt to read configuration and process details for a local node
// from the specified directory.
func ReadNode(dataDir string) (*LocalNode, error) {
	node := NewLocalNode(dataDir)
	if _, err := os.Stat(node.GetConfigPath()); err != nil {
		return nil, fmt.Errorf("unexpected error checking for local node config file: %w", err)
	}
	if err := node.ReadAll(); err != nil {
		return nil, err
	}
	return node, nil
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
	return cast.ToString(n.Flags[cfg.DataDirKey])
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
	return filepath.Join(n.GetDataDir(), "process.json")
}

func (n *LocalNode) ReadProcessContext() error {
	path := n.GetProcessContextPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// The absence of the process context file indicates the node is not running
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
	if err := n.ReadProcessContext(); err != nil {
		return err
	}
	return nil
}

func (n *LocalNode) Start(w io.Writer, defaultExecPath string) error {
	// Ensure a stale process context file is removed so that the
	// creation of a new file can indicate node start.
	if err := os.Remove(n.GetProcessContextPath()); err != nil && !os.IsNotExist(err) {
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
		} else {
			_, _ = fmt.Fprintf(w, "node %q finished: %v\n", n.NodeID, cmd.ProcessState.String())
		}
	}()

	if _, err := fmt.Fprintf(w, "Started %s\n", n.NodeID); err != nil {
		return err
	}

	return nil
}

func (n *LocalNode) Stop() error {
	pid := n.PID
	if pid == 0 {
		// Nothing to do
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process to stop: %w", err)
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("process.Signal(0) on pid %d returned: %w", pid, err)
	}
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("failed to kill pid %d: %w", pid, err)
	}
	return nil
}

func (n *LocalNode) IsHealthy(ctx context.Context) (bool, error) {
	// Ensure the API URI is available
	if len(n.URI) == 0 {
		if err := n.ReadProcessContext(); err != nil {
			return false, fmt.Errorf("failed to read process context: %v", err)
		}
		if len(n.URI) == 0 {
			return false, nil
		}
	}

	// Check that the process is still alive
	proc, err := os.FindProcess(n.PID)
	if err != nil {
		return false, fmt.Errorf("failed to find process: %w", err)
	}
	err = proc.Signal(syscall.Signal(0))
	switch err {
	case nil:
		// Process is alive
	case syscall.ESRCH:
		// Process is dead
		return false, errors.New("process not running")
	default:
		return false, errors.New("process running but query operation not permitted")
	}

	// Check that the node is reporting healthy
	health, err := health.NewClient(n.URI).Health(ctx, nil)
	if err != nil {
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
	return health.Healthy, nil
}

func (n *LocalNode) WaitForProcessContext(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to load process context for node %q before timeout", n.NodeID)
		default:
			// Attempt to read process context from disk
			err := n.ReadProcessContext()
			if err != nil {
				return fmt.Errorf("failed to read process context for node %q: %w", n.NodeID, err)
			}
		}
		if len(n.URI) > 0 {
			break
		}
		time.Sleep(time.Millisecond * 200)
	}
	return nil
}
