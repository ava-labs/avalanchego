// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	AvalancheGoPathEnvName = "AVALANCHEGO_PATH"

	defaultNodeInitTimeout = 10 * time.Second
)

var (
	AvalancheGoPluginDirEnvName = config.EnvVarName(config.EnvPrefix, config.PluginDirKey)

	errNodeAlreadyRunning   = errors.New("failed to start node: node is already running")
	errNotRunning           = errors.New("node is not running")
	errMissingRuntimeConfig = errors.New("node is missing runtime configuration")
)

// Defines local-specific node configuration. Supports setting default
// and node-specific values.
type ProcessRuntime struct {
	node *Node

	// PID of the node process
	pid int
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
		return fmt.Errorf("failed to read node process context: %w", err)
	}
	processContext := node.ProcessContext{}
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
func (p *ProcessRuntime) Start(ctx context.Context) error {
	log := p.node.network.log

	// Avoid attempting to start an already running node.
	proc, err := p.getProcess()
	if err != nil {
		return fmt.Errorf("failed to retrieve existing process: %w", err)
	}
	if proc != nil {
		return errNodeAlreadyRunning
	}

	runtimeConfig := p.node.getRuntimeConfig().Process
	if runtimeConfig == nil {
		return errMissingRuntimeConfig
	}

	// Attempt to check for rpc version compatibility
	if err := checkVMBinaries(log, p.node.network.Subnets, runtimeConfig); err != nil {
		return err
	}

	// Ensure a stale process context file is removed so that the
	// creation of a new file can indicate node start.
	if err := os.Remove(p.getProcessContextPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove stale process context file: %w", err)
	}

	// All arguments are provided in the flags file
	cmd := exec.Command(runtimeConfig.AvalancheGoPath, "--config-file", p.node.GetFlagsPath()) // #nosec G204
	// Ensure process is detached from the parent process so that an error in the parent will not affect the child
	configureDetachedProcess(cmd)

	if err := cmd.Start(); err != nil {
		return err
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
		return fmt.Errorf("failed to start local node: %w", err)
	}

	log.Info("started local node",
		zap.Stringer("nodeID", p.node.NodeID),
		zap.String("dataDir", p.node.DataDir),
		zap.Bool("isEphemeral", p.node.IsEphemeral),
	)

	// Configure collection of metrics and logs
	return p.writeMonitoringConfig()
}

// Signals the node process to stop.
func (p *ProcessRuntime) InitiateStop(_ context.Context) error {
	proc, err := p.getProcess()
	if err != nil {
		return fmt.Errorf("failed to retrieve process to stop: %w", err)
	}
	if proc == nil {
		// Already stopped
		return p.removeMonitoringConfig()
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to pid %d: %w", p.pid, err)
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
			return fmt.Errorf("failed to retrieve process: %w", err)
		}
		if proc == nil {
			return p.removeMonitoringConfig()
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see node process stop %q before timeout: %w", p.node.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *ProcessRuntime) IsHealthy(ctx context.Context) (bool, error) {
	// Check that the node process is running as a precondition for
	// checking health. getProcess will also ensure that the node's
	// API URI is current.
	proc, err := p.getProcess()
	if err != nil {
		return false, fmt.Errorf("failed to determine process status: %w", err)
	}
	if proc == nil {
		return false, errNotRunning
	}

	healthReply, err := CheckNodeHealth(ctx, p.node.URI)
	if err != nil {
		return false, err
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
			return fmt.Errorf("failed to read process context for node %q: %w", p.node.NodeID, err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to load process context for node %q: %w", p.node.NodeID, context.Cause(ctx))
		case <-ticker.C:
		}
	}
	return nil
}

// Retrieve the node process if it is running. As part of determining
// process liveness, the node's process context will be refreshed if
// live or cleared if not running.
func (p *ProcessRuntime) getProcess() (*os.Process, error) {
	// Read the process context to ensure freshness. The node may have
	// stopped or been restarted since last read.
	if err := p.readState(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to read process context: %w", err)
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

// Write monitoring configuration enabling collection of metrics and logs from the node.
func (p *ProcessRuntime) writeMonitoringConfig() error {
	// Ensure labeling that uniquely identifies the node and its network
	commonLabels := FlagsMap{
		// Explicitly setting an instance label avoids the default
		// behavior of using the node's URI since the URI isn't
		// guaranteed stable (e.g. port may change after restart).
		"instance":          p.node.GetUniqueID(),
		"network_uuid":      p.node.network.UUID,
		"node_id":           p.node.NodeID.String(),
		"is_ephemeral_node": strconv.FormatBool(p.node.IsEphemeral),
		"network_owner":     p.node.network.Owner,
	}
	commonLabels.SetDefaults(githubLabelsFromEnv())

	prometheusConfig := []ConfigMap{
		{
			"targets": []string{strings.TrimPrefix(p.node.URI, "http://")},
			"labels":  commonLabels,
		},
	}
	if err := p.writeMonitoringConfigFile(prometheusCmd, prometheusConfig); err != nil {
		return err
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
		return "", err
	}
	return filepath.Join(serviceDiscoveryDir, fmt.Sprintf("%s_%s.json", p.node.network.UUID, p.node.NodeID)), nil
}

// Ensure the removal of the monitoring configuration files for this node.
func (p *ProcessRuntime) removeMonitoringConfig() error {
	for _, name := range []string{promtailCmd, prometheusCmd} {
		configPath, err := p.getMonitoringConfigPath(name)
		if err != nil {
			return err
		}
		if err := os.Remove(configPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to remove %s config: %w", name, err)
		}
	}
	return nil
}

// Write the configuration for a type of monitoring (e.g. prometheus, promtail).
func (p *ProcessRuntime) writeMonitoringConfigFile(name string, config []ConfigMap) error {
	configPath, err := p.getMonitoringConfigPath(name)
	if err != nil {
		return err
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create %s service discovery dir: %w", name, err)
	}

	bytes, err := DefaultJSONMarshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal %s config: %w", name, err)
	}

	if err := os.WriteFile(configPath, bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write %s config: %w", name, err)
	}

	return nil
}

func (p *ProcessRuntime) GetLocalURI(_ context.Context) (string, func(), error) {
	return p.node.URI, func() {}, nil
}

func (p *ProcessRuntime) GetLocalStakingAddress(_ context.Context) (netip.AddrPort, func(), error) {
	return p.node.StakingAddress, func() {}, nil
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

func githubLabelsFromEnv() map[string]string {
	return map[string]string{
		// prometheus/promtail ignore empty values so including these
		// labels with empty values outside of a github worker (where
		// the env vars will not be set) should not be a problem.
		"gh_repo":        os.Getenv("GH_REPO"),
		"gh_workflow":    os.Getenv("GH_WORKFLOW"),
		"gh_run_id":      os.Getenv("GH_RUN_ID"),
		"gh_run_number":  os.Getenv("GH_RUN_NUMBER"),
		"gh_run_attempt": os.Getenv("GH_RUN_ATTEMPT"),
		"gh_job_id":      os.Getenv("GH_JOB_ID"),
	}
}
