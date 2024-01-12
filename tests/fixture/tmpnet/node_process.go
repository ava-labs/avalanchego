// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

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

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/node"
)

const (
	AvalancheGoPathEnvName = "AVALANCHEGO_PATH"

	defaultNodeInitTimeout = 10 * time.Second
)

var errNodeAlreadyRunning = errors.New("failed to start node: node is already running")

func checkNodeHealth(ctx context.Context, uri string) (bool, error) {
	// Check that the node is reporting healthy
	health, err := health.NewClient(uri).Health(ctx, nil)
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

// Defines local-specific node configuration. Supports setting default
// and node-specific values.
type NodeProcess struct {
	node *Node

	// PID of the node process
	pid int
}

func (p *NodeProcess) setProcessContext(processContext node.NodeProcessContext) {
	p.pid = processContext.PID
	p.node.URI = processContext.URI
	p.node.StakingAddress = processContext.StakingAddress
}

func (p *NodeProcess) readState() error {
	path := p.getProcessContextPath()
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		// The absence of the process context file indicates the node is not running
		p.setProcessContext(node.NodeProcessContext{})
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
	p.setProcessContext(processContext)
	return nil
}

// Start waits for the process context to be written which
// indicates that the node will be accepting connections on
// its staking port. The network will start faster with this
// synchronization due to the avoidance of exponential backoff
// if a node tries to connect to a beacon that is not ready.
func (p *NodeProcess) Start(w io.Writer) error {
	// Avoid attempting to start an already running node.
	proc, err := p.getProcess()
	if err != nil {
		return fmt.Errorf("failed to retrieve existing process: %w", err)
	}
	if proc != nil {
		return errNodeAlreadyRunning
	}

	// Ensure a stale process context file is removed so that the
	// creation of a new file can indicate node start.
	if err := os.Remove(p.getProcessContextPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove stale process context file: %w", err)
	}

	cmd := exec.Command(p.node.RuntimeConfig.AvalancheGoPath, "--config-file", p.node.getFlagsPath()) // #nosec G204
	if err := cmd.Start(); err != nil {
		return err
	}

	// Determine appropriate level of node description detail
	dataDir := p.node.getDataDir()
	nodeDescription := fmt.Sprintf("node %q", p.node.NodeID)
	if p.node.IsEphemeral {
		nodeDescription = "ephemeral " + nodeDescription
	}
	nonDefaultNodeDir := filepath.Base(dataDir) != p.node.NodeID.String()
	if nonDefaultNodeDir {
		// Only include the data dir if its base is not the default (the node ID)
		nodeDescription = fmt.Sprintf("%s with path: %s", nodeDescription, dataDir)
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
	if err := p.waitForProcessContext(context.Background()); err != nil {
		return fmt.Errorf("failed to start local node: %w", err)
	}

	_, err = fmt.Fprintf(w, "Started %s\n", nodeDescription)
	return err
}

// Signals the node process to stop.
func (p *NodeProcess) InitiateStop() error {
	proc, err := p.getProcess()
	if err != nil {
		return fmt.Errorf("failed to retrieve process to stop: %w", err)
	}
	if proc == nil {
		// Already stopped
		return nil
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to pid %d: %w", p.pid, err)
	}
	return nil
}

// Waits for the node process to stop.
func (p *NodeProcess) WaitForStopped(ctx context.Context) error {
	ticker := time.NewTicker(defaultNodeTickerInterval)
	defer ticker.Stop()
	for {
		proc, err := p.getProcess()
		if err != nil {
			return fmt.Errorf("failed to retrieve process: %w", err)
		}
		if proc == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see node process stop %q before timeout: %w", p.node.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *NodeProcess) IsHealthy(ctx context.Context) (bool, error) {
	// Check that the node process is running as a precondition for
	// checking health. getProcess will also ensure that the node's
	// API URI is current.
	proc, err := p.getProcess()
	if err != nil {
		return false, fmt.Errorf("failed to determine process status: %w", err)
	}
	if proc == nil {
		return false, ErrNotRunning
	}

	return checkNodeHealth(ctx, p.node.URI)
}

func (p *NodeProcess) getProcessContextPath() string {
	return filepath.Join(p.node.getDataDir(), config.DefaultProcessContextFilename)
}

func (p *NodeProcess) waitForProcessContext(ctx context.Context) error {
	ticker := time.NewTicker(defaultNodeTickerInterval)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, defaultNodeInitTimeout)
	defer cancel()
	for len(p.node.URI) == 0 {
		err := p.readState()
		if err != nil {
			return fmt.Errorf("failed to read process context for node %q: %w", p.node.NodeID, err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to load process context for node %q before timeout: %w", p.node.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

// Retrieve the node process if it is running. As part of determining
// process liveness, the node's process context will be refreshed if
// live or cleared if not running.
func (p *NodeProcess) getProcess() (*os.Process, error) {
	// Read the process context to ensure freshness. The node may have
	// stopped or been restarted since last read.
	if err := p.readState(); err != nil {
		return nil, fmt.Errorf("failed to read process context: %w", err)
	}

	if p.pid == 0 {
		// Process is not running
		return nil, nil
	}

	proc, err := os.FindProcess(p.pid)
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
