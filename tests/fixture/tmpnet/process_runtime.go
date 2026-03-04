// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	AvalancheGoPathEnvName = "AVALANCHEGO_PATH"

	defaultNodeInitTimeout = 10 * time.Second
)

var (
	AvalancheGoPluginDirEnvName = config.EnvVarName(config.EnvPrefix, config.PluginDirKey)

	errNodeAlreadyRunning = errors.New("failed to start node: node is already running")
	errNotRunning         = errors.New("node is not running")
)

type ProcessRuntimeConfig struct {
	AvalancheGoPath   string `json:"avalancheGoPath,omitempty"`
	PluginDir         string `json:"pluginDir,omitempty"`
	ReuseDynamicPorts bool   `json:"reuseDynamicPorts,omitempty"`
}

// Defines local-specific node configuration. Supports setting default
// and node-specific values.
type ProcessRuntime struct {
	node *Node

	// PID of the node process
	pid int
}

func (p *ProcessRuntime) getRuntimeConfig() *ProcessRuntimeConfig {
	return p.node.getRuntimeConfig().Process
}

func (p *ProcessRuntime) setProcessContext(processContext node.ProcessContext) {
	p.pid = processContext.PID
	p.node.URI = processContext.URI
	p.node.StakingAddress = processContext.StakingAddress
}

func (p *ProcessRuntime) readState(_ context.Context) error {
	path := p.getProcessContextPath()
	bytes, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// The absence of the process context file indicates the node is not running
		p.setProcessContext(node.ProcessContext{})
		return nil
	} else if err != nil {
		return stacktrace.Errorf("failed to read node process context: %w", err)
	}
	processContext := node.ProcessContext{}
	if err := json.Unmarshal(bytes, &processContext); err != nil {
		return stacktrace.Errorf("failed to unmarshal node process context: %w", err)
	}
	p.setProcessContext(processContext)
	return nil
}

// Start waits for the process context to be written which
// indicates that the node will be accepting connections on
// its staking port. The network will start faster with this
// synchronization due to the avoidance of exponential backoff
// if a node tries to connect to a beacon that is not ready.
func (p *ProcessRuntime) Start(ctx context.Context) error {
	log := p.node.network.log

	// Avoid attempting to start an already running node.
	proc, err := p.getProcess()
	if err != nil {
		return stacktrace.Errorf("failed to retrieve existing process: %w", err)
	}
	if proc != nil {
		return stacktrace.Wrap(errNodeAlreadyRunning)
	}

	runtimeConfig := p.getRuntimeConfig()

	// Attempt to check for rpc version compatibility
	if err := checkVMBinaries(log, p.node.network.Subnets, runtimeConfig); err != nil {
		return stacktrace.Wrap(err)
	}

	// Ensure a stale process context file is removed so that the
	// creation of a new file can indicate node start.
	if err := os.Remove(p.getProcessContextPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return stacktrace.Errorf("failed to remove stale process context file: %w", err)
	}

	if err := p.writeFlags(); err != nil {
		return stacktrace.Errorf("writing node flags: %w", err)
	}

	// All arguments are provided in the flags file
	cmd := exec.Command(runtimeConfig.AvalancheGoPath, "--config-file", p.node.GetFlagsPath())
	// Ensure process is detached from the parent process so that an error in the parent will not affect the child
	configureDetachedProcess(cmd)

	if err := cmd.Start(); err != nil {
		return stacktrace.Wrap(err)
	}

	// Watch the node's main.log file in the background for FATAL log entries that indicate
	// a configuration error preventing startup. Such a log entry will be provided to the
	// cancelWithCause function so that waitForProcessContext can exit early with an error
	// that includes the log entry.
	ctx, cancelWithCause := context.WithCancelCause(ctx)
	defer cancelWithCause(nil)
	logPath := p.node.DataDir + "/logs/main.log"
	go watchLogFileForFatal(ctx, cancelWithCause, log, logPath)

	// A node writes a process context file on start. If the file is not
	// found in a reasonable amount of time, the node is unlikely to have
	// started successfully.
	if err := p.waitForProcessContext(ctx); err != nil {
		return stacktrace.Errorf("failed to start local node: %w", err)
	}

	log.Info("started local node",
		zap.Stringer("nodeID", p.node.NodeID),
		zap.String("dataDir", p.node.DataDir),
		zap.Bool("isEphemeral", p.node.IsEphemeral),
	)

	// Configure collection of metrics and logs
	return p.writeMonitoringConfig()
}

func (p *ProcessRuntime) writeFlags() error {
	flags, err := p.node.composeFlags()
	if err != nil {
		return stacktrace.Errorf("failed to compose node flags: %w", err)
	}

	flags.SetDefaults(FlagsMap{
		config.DataDirKey: p.node.DataDir,
		// Use dynamic port allocation
		config.HTTPPortKey:    "0",
		config.StakingPortKey: "0",
		// Binding to localhost on macos avoids having a permission
		// dialog pop up for every node that tries to bind to
		// non-localhost interfaces
		config.PublicIPKey:    "127.0.0.1",
		config.HTTPHostKey:    "127.0.0.1",
		config.StakingHostKey: "127.0.0.1",
	})

	runtimeConfig := p.getRuntimeConfig()

	// Only configure the plugin dir with a non-empty value to ensure the use of the
	// default value (`[datadir]/plugins`) when no plugin dir is configured.
	if len(runtimeConfig.PluginDir) > 0 {
		flags.SetDefault(config.PluginDirKey, runtimeConfig.PluginDir)
	}

	// Ensure a non-empty plugin directory exists or the node will fail to start.
	pluginDir := flags[config.PluginDirKey]
	if len(pluginDir) > 0 {
		if err := os.MkdirAll(pluginDir, perms.ReadWriteExecute); err != nil {
			return stacktrace.Errorf("failed to create plugin dir: %w", err)
		}
	}

	bytes, err := DefaultJSONMarshal(flags)
	if err != nil {
		return stacktrace.Errorf("failed to marshal node flags: %w", err)
	}
	if err := os.WriteFile(p.node.GetFlagsPath(), bytes, perms.ReadWrite); err != nil {
		return stacktrace.Errorf("failed to write node flags: %w", err)
	}
	return nil
}

// Signals the node process to stop.
func (p *ProcessRuntime) InitiateStop(_ context.Context) error {
	proc, err := p.getProcess()
	if err != nil {
		return stacktrace.Errorf("failed to retrieve process to stop: %w", err)
	}
	if proc == nil {
		// Already stopped
		return p.removeMonitoringConfig()
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return stacktrace.Errorf("failed to send SIGTERM to pid %d: %w", p.pid, err)
	}
	return nil
}

// Waits for the node process to stop.
func (p *ProcessRuntime) WaitForStopped(ctx context.Context) error {
	ticker := time.NewTicker(defaultNodeTickerInterval)
	defer ticker.Stop()
	for {
		proc, err := p.getProcess()
		if err != nil {
			return stacktrace.Errorf("failed to retrieve process: %w", err)
		}
		if proc == nil {
			return p.removeMonitoringConfig()
		}

		select {
		case <-ctx.Done():
			return stacktrace.Errorf("failed to see node process stop %q before timeout: %w", p.node.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Restarts the node
func (p *ProcessRuntime) Restart(ctx context.Context) error {
	if p.getRuntimeConfig().ReuseDynamicPorts {
		// Attempt to save the API port currently being used so the
		// restarted node can reuse it. This may result in the node
		// failing to start if the operating system allocates the port
		// to a different process between node stop and start.
		if err := p.saveAPIPort(); err != nil {
			return stacktrace.Wrap(err)
		}
	}
	if err := p.node.Stop(ctx); err != nil {
		return stacktrace.Errorf("failed to stop node %s: %w", p.node.NodeID, err)
	}
	if err := p.Start(ctx); err != nil {
		return stacktrace.Errorf("failed to start node %s: %w", p.node.NodeID, err)
	}
	return nil
}

func (p *ProcessRuntime) IsHealthy(ctx context.Context) (bool, error) {
	// Check that the node process is running as a precondition for
	// checking health. getProcess will also ensure that the node's
	// API URI is current.
	proc, err := p.getProcess()
	if err != nil {
		return false, stacktrace.Errorf("failed to determine process status: %w", err)
	}
	if proc == nil {
		return false, stacktrace.Wrap(errNotRunning)
	}

	healthReply, err := CheckNodeHealth(ctx, p.node.URI)
	if err != nil {
		return false, stacktrace.Wrap(err)
	}
	return healthReply.Healthy, nil
}

func (p *ProcessRuntime) getProcessContextPath() string {
	return filepath.Join(p.node.DataDir, config.DefaultProcessContextFilename)
}

func (p *ProcessRuntime) waitForProcessContext(ctx context.Context) error {
	ticker := time.NewTicker(defaultNodeTickerInterval)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, defaultNodeInitTimeout)
	defer cancel()
	for len(p.node.URI) == 0 {
		err := p.readState(ctx)
		if err != nil {
			return stacktrace.Errorf("failed to read process context for node %q: %w", p.node.NodeID, err)
		}

		select {
		case <-ctx.Done():
			return stacktrace.Errorf("failed to load process context for node %q: %w", p.node.NodeID, context.Cause(ctx))
		case <-ticker.C:
		}
	}
	return nil
}

// Retrieve the node process if it is running. As part of determining
// process liveness, the node's process context will be refreshed if
// live or cleared if not running.
func (p *ProcessRuntime) getProcess() (*os.Process, error) {
	// This context is not used but a non-nil value must be supplied to satisfy the linter
	ctx := context.TODO()
	// Read the process context to ensure freshness. The node may have
	// stopped or been restarted since last read.
	if err := p.readState(ctx); err != nil {
		return nil, stacktrace.Errorf("failed to read process context: %w", err)
	}

	if p.pid == 0 {
		// Process is not running
		return nil, nil
	}

	return getProcess(p.pid)
}

// getProcess retrieves the process if it is running.
func getProcess(pid int) (*os.Process, error) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, stacktrace.Errorf("failed to find process: %w", err)
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
	return nil, stacktrace.Errorf("failed to determine process status: %w", err)
}

// Write monitoring configuration enabling collection of metrics and logs from the node.
func (p *ProcessRuntime) writeMonitoringConfig() error {
	// Ensure labeling that uniquely identifies the node and its network
	commonLabels := p.node.getMonitoringLabels()

	prometheusConfig := []ConfigMap{
		{
			"targets": []string{strings.TrimPrefix(p.node.URI, "http://")},
			"labels":  commonLabels,
		},
	}
	if err := p.writeMonitoringConfigFile(prometheusCmd, prometheusConfig); err != nil {
		return stacktrace.Wrap(err)
	}

	promtailLabels := map[string]string{}
	maps.Copy(promtailLabels, commonLabels)
	promtailLabels["__path__"] = filepath.Join(p.node.DataDir, "logs", "*.log")
	promtailConfig := []ConfigMap{
		{
			"targets": []string{"localhost"},
			"labels":  promtailLabels,
		},
	}
	return p.writeMonitoringConfigFile(promtailCmd, promtailConfig)
}

// Return the path for this node's prometheus configuration.
func (p *ProcessRuntime) getMonitoringConfigPath(name string) (string, error) {
	// Ensure a unique filename to allow config files to be added and removed
	// by multiple nodes without conflict.
	serviceDiscoveryDir, err := getServiceDiscoveryDir(name)
	if err != nil {
		return "", stacktrace.Wrap(err)
	}
	return filepath.Join(serviceDiscoveryDir, fmt.Sprintf("%s_%s.json", p.node.network.UUID, p.node.NodeID)), nil
}

// Ensure the removal of the monitoring configuration files for this node.
func (p *ProcessRuntime) removeMonitoringConfig() error {
	for _, name := range []string{promtailCmd, prometheusCmd} {
		configPath, err := p.getMonitoringConfigPath(name)
		if err != nil {
			return stacktrace.Wrap(err)
		}
		if err := os.Remove(configPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return stacktrace.Errorf("failed to remove %s config: %w", name, err)
		}
	}
	return nil
}

// Write the configuration for a type of monitoring (e.g. prometheus, promtail).
func (p *ProcessRuntime) writeMonitoringConfigFile(name string, config []ConfigMap) error {
	configPath, err := p.getMonitoringConfigPath(name)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, perms.ReadWriteExecute); err != nil {
		return stacktrace.Errorf("failed to create %s service discovery dir: %w", name, err)
	}

	bytes, err := DefaultJSONMarshal(config)
	if err != nil {
		return stacktrace.Errorf("failed to marshal %s config: %w", name, err)
	}

	if err := os.WriteFile(configPath, bytes, perms.ReadWrite); err != nil {
		return stacktrace.Errorf("failed to write %s config: %w", name, err)
	}

	return nil
}

// GetAccessibleURI returns the URI that can be used to access the node's API.
func (p *ProcessRuntime) GetAccessibleURI() string {
	return p.node.URI
}

func (p *ProcessRuntime) GetAccessibleStakingAddress(_ context.Context) (netip.AddrPort, func(), error) {
	return p.node.StakingAddress, func() {}, nil
}

// Saves the currently allocated API port to the node's configuration
// for use across restarts.
func (p *ProcessRuntime) saveAPIPort() error {
	hostPort := strings.TrimPrefix(p.node.URI, "http://")
	if len(hostPort) == 0 {
		// Without an API URI there is nothing to save
		return nil
	}
	_, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	p.node.Flags[config.HTTPPortKey] = port
	return nil
}

// watchLogFileForFatal waits for the specified file path to exist and then checks each of
// its lines for the string 'FATAL' until such a line is observed or the provided context
// is canceled. If line containing 'FATAL' is encountered, it will be provided as an error
// to the provided cancelWithCause function.
//
// Errors encountered while looking for FATAL log entries are considered potential rather
// than positive indications of failure and are printed to the provided writer instead of
// being provided to the cancelWithCause function.
func watchLogFileForFatal(ctx context.Context, cancelWithCause context.CancelCauseFunc, log logging.Logger, path string) {
	waitInterval := 100 * time.Millisecond
	// Wait for the file to exist
	fileExists := false
	for !fileExists {
		select {
		case <-ctx.Done():
			return
		default:
			if _, err := os.Stat(path); os.IsNotExist(err) {
				// File does not exist yet - wait and try again
				time.Sleep(waitInterval)
			} else {
				fileExists = true
			}
		}
	}

	// Open the file
	file, err := os.Open(path)
	if err != nil {
		log.Error("failed to open log file",
			zap.String("path", path),
			zap.Error(err),
		)
		return
	}
	defer file.Close()

	// Scan for lines in the file containing 'FATAL'
	reader := bufio.NewReader(file)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Read a line from the file
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					// If end of file is reached, wait and try again
					time.Sleep(waitInterval)
					continue
				} else {
					log.Error("failed to read log file",
						zap.String("path", path),
						zap.Error(err),
					)
					return
				}
			}
			if strings.Contains(line, "FATAL") {
				cancelWithCause(errors.New(line))
				return
			}
		}
	}
}
