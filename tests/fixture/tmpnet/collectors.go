// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

type configGeneratorFunc func(workingDir string, username string, password string) string

const (
	collectorTickerInterval = 100 * time.Millisecond

	prometheusScrapeInterval = 10 * time.Second

	prometheusCmd = "prometheus"
	promtailCmd   = "promtail"

	// Use a delay slightly longer than the scrape interval to ensure a final scrape before shutdown
	NetworkShutdownDelay = prometheusScrapeInterval + 2*time.Second
)

// EnsureCollectorsRunning ensures collectors are running to collect logs and metrics from local nodes.
func EnsureCollectorsRunning(ctx context.Context, log logging.Logger) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("unable to start collectors with a context without a deadline")
	}
	if err := ensurePrometheusRunning(ctx, log); err != nil {
		return err
	}
	if err := ensurePromtailRunning(ctx, log); err != nil {
		return err
	}

	log.Info("To stop: tmpnetctl stop-collectors")

	return nil
}

// EnsureCollectorsStopped ensures collectors are not running.
func EnsureCollectorsStopped(ctx context.Context, log logging.Logger) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("unable to start collectors with a context without a deadline")
	}
	for _, cmdName := range []string{prometheusCmd, promtailCmd} {
		// Determine if the process is running
		workingDir, err := getWorkingDir(cmdName)
		if err != nil {
			return err
		}
		pidPath := getPIDPath(workingDir)
		proc, err := processFromPIDFile(workingDir, pidPath)
		if err != nil {
			return err
		}
		if proc == nil {
			log.Info("collector not running",
				zap.String("cmd", cmdName),
			)
			continue
		}

		log.Info("sending SIGTERM to collector process",
			zap.String("cmdName", cmdName),
			zap.Int("pid", proc.Pid),
		)
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM to pid %d: %w", proc.Pid, err)
		}

		log.Info("waiting for collector process to stop",
			zap.String("cmdName", cmdName),
			zap.Int("pid", proc.Pid),
		)
		ticker := time.NewTicker(collectorTickerInterval)
		defer ticker.Stop()
		for {
			p, err := getProcess(proc.Pid)
			if err != nil {
				return fmt.Errorf("failed to retrieve process: %w", err)
			}
			if p == nil {
				// Process is no longer running

				// Attempt to clear the PID file. Not critical that it is removed, just good housekeeping.
				if err := clearStalePIDFile(log, cmdName, pidPath); err != nil {
					log.Warn("failed to remove stale PID file",
						zap.String("cmd", cmdName),
						zap.String("pidFile", pidPath),
						zap.Error(err),
					)
				}

				break
			}

			select {
			case <-ctx.Done():
				return fmt.Errorf("failed to see %s stop before timeout: %w", cmdName, ctx.Err())
			case <-ticker.C:
			}
		}
		log.Info("collector stopped",
			zap.String("cmdName", cmdName),
		)
	}

	return nil
}

// ensurePrometheusRunning ensures an agent-mode prometheus process is running to collect metrics from local nodes.
func ensurePrometheusRunning(ctx context.Context, log logging.Logger) error {
	return ensureCollectorRunning(
		ctx,
		log,
		prometheusCmd,
		"--config.file=prometheus.yaml --storage.agent.path=./data --web.listen-address=localhost:0 --enable-feature=agent",
		"PROMETHEUS",
		func(workingDir string, username string, password string) string {
			return fmt.Sprintf(`
global:
  scrape_interval: %v     # Default is every 1 minute.
  evaluation_interval: 10s # The default is every 1 minute.
  scrape_timeout: 5s       # The default is every 10s

scrape_configs:
  - job_name: "avalanchego"
    metrics_path: "/ext/metrics"
    file_sd_configs:
      - files:
          - '%s/file_sd_configs/*.json'

remote_write:
  - url: "https://prometheus-poc.avax-dev.network/api/v1/write"
    basic_auth:
      username: "%s"
      password: "%s"
`, prometheusScrapeInterval, workingDir, username, password)
		},
	)
}

// ensurePromtailRunning ensures a promtail process is running to collect logs from local nodes.
func ensurePromtailRunning(ctx context.Context, log logging.Logger) error {
	return ensureCollectorRunning(
		ctx,
		log,
		promtailCmd,
		"-config.file=promtail.yaml",
		"LOKI",
		func(workingDir string, username string, password string) string {
			return fmt.Sprintf(`
server:
  http_listen_port: 0
  grpc_listen_port: 0

positions:
  filename: %s/positions.yaml

client:
  url: "https://loki-poc.avax-dev.network/api/prom/push"
  basic_auth:
    username: "%s"
    password: "%s"

scrape_configs:
  - job_name: "avalanchego"
    file_sd_configs:
      - files:
          - '%s/file_sd_configs/*.json'
`, workingDir, username, password, workingDir)
		},
	)
}

func getWorkingDir(cmdName string) (string, error) {
	tmpnetDir, err := getTmpnetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpnetDir, cmdName), nil
}

func getPIDPath(workingDir string) string {
	return filepath.Join(workingDir, "run.pid")
}

// ensureCollectorRunning starts a collector process if it is not already running.
func ensureCollectorRunning(
	ctx context.Context,
	log logging.Logger,
	cmdName string,
	args string,
	baseEnvName string,
	configGenerator configGeneratorFunc,
) error {
	// Determine paths
	workingDir, err := getWorkingDir(cmdName)
	if err != nil {
		return err
	}
	pidPath := getPIDPath(workingDir)

	// Ensure required paths exist
	if err := os.MkdirAll(workingDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create %s dir: %w", cmdName, err)
	}
	if err := os.MkdirAll(filepath.Join(workingDir, "file_sd_configs"), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create %s file_sd_configs dir: %w", cmdName, err)
	}

	// Check if the process is already running
	if process, err := processFromPIDFile(cmdName, pidPath); err != nil {
		return err
	} else if process != nil {
		log.Info(cmdName + " is already running")
		return nil
	}

	// Clear any stale pid file
	if err := clearStalePIDFile(log, cmdName, pidPath); err != nil {
		return err
	}

	// Check if the specified command is available in the path
	if _, err := exec.LookPath(cmdName); err != nil {
		return fmt.Errorf("%s command not found. Maybe run 'nix develop'?", cmdName)
	}

	// Write the collector config file
	if err := writeConfigFile(log, cmdName, workingDir, baseEnvName, configGenerator); err != nil {
		return err
	}

	// Start the collector
	return startCollector(ctx, log, cmdName, args, workingDir, pidPath)
}

// processFromPIDFile attempts to retrieve a running process from the specified PID file.
func processFromPIDFile(cmdName string, pidPath string) (*os.Process, error) {
	pid, err := getPID(cmdName, pidPath)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, nil
	}
	return getProcess(pid)
}

// getPID attempts to read the PID of the collector from a PID file.
func getPID(cmdName string, pidPath string) (int, error) {
	pidData, err := os.ReadFile(pidPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("failed to read %s PID file %s: %w", cmdName, pidPath, err)
	}
	if len(pidData) == 0 {
		return 0, nil
	}
	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s PID: %w", cmdName, err)
	}
	return pid, nil
}

// clearStalePIDFile remove an existing pid file to avoid conflicting with a new process.
func clearStalePIDFile(log logging.Logger, cmdName string, pidPath string) error {
	if err := os.Remove(pidPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to remove stale pid file: %w", err)
		}
	} else {
		log.Info("deleted stale "+cmdName+" pid file",
			zap.String("path", pidPath),
		)
	}
	return nil
}

// writeConfigFile writes the configuration file for a collector
func writeConfigFile(
	log logging.Logger,
	cmdName string,
	workingDir string,
	baseEnvName string,
	configGenerator configGeneratorFunc,
) error {
	// Retrieve the credentials for the command
	username, password, err := getCredentials(baseEnvName)
	if err != nil {
		return err
	}

	// Generate configuration for the command to its working dir
	confFilename := cmdName + ".yaml"
	confPath := filepath.Join(workingDir, confFilename)
	log.Info("writing "+cmdName+" config",
		zap.String("path", confPath),
	)
	config := configGenerator(workingDir, username, password)
	return os.WriteFile(confPath, []byte(config), perms.ReadWrite)
}

// getCredentials retrieves the username and password for the given base env name.
func getCredentials(baseEnvName string) (string, string, error) {
	usernameEnvVar := baseEnvName + "_USERNAME"
	username := GetEnvWithDefault(usernameEnvVar, "")
	if len(username) == 0 {
		return "", "", fmt.Errorf("%s env var not set", usernameEnvVar)
	}
	passwordEnvVar := baseEnvName + "_PASSWORD"
	password := GetEnvWithDefault(passwordEnvVar, "")
	if len(password) == 0 {
		return "", "", fmt.Errorf("%s var not set", passwordEnvVar)
	}
	return username, password, nil
}

// Start a collector. Use bash to execute the command in the background and enable
// stderr and stdout redirection to a log file.
//
// Ideally this would be possible without bash, but it does not seem possible to
// have this process open a log file, set cmd.Stdout cmd.Stderr to that file, and
// then have the child process be able to write to that file once the parent
// process exits. Attempting to do so resulted in an empty log file.
func startCollector(
	ctx context.Context,
	log logging.Logger,
	cmdName string,
	args string,
	workingDir string,
	pidPath string,
) error {
	fullCmd := "nohup " + cmdName + " " + args + " > " + cmdName + ".log 2>&1 & echo -n \"$!\" > " + pidPath
	log.Info("starting "+cmdName,
		zap.String("workingDir", workingDir),
		zap.String("fullCmd", fullCmd),
	)

	cmd := exec.Command("bash", "-c", fullCmd)
	configureDetachedProcess(cmd) // Ensure the child process will outlive its parent
	cmd.Dir = workingDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", cmdName, err)
	}

	// Wait for PID file to be written. It's not enough to check for the PID of cmd
	// because the PID we want is a child of the process that cmd represents.
	if pid, err := waitForPIDFile(ctx, cmdName, pidPath); err != nil {
		return err
	} else {
		log.Info(cmdName+" started",
			zap.String("pid", pid),
		)
	}

	return nil
}

// waitForPIDFile waits for the PID file to be written as an indication of process start.
func waitForPIDFile(ctx context.Context, cmdName string, pidPath string) (string, error) {
	var (
		ticker = time.NewTicker(collectorTickerInterval)
		pid    string
	)
	defer ticker.Stop()
	for {
		if fileExistsAndNotEmpty(pidPath) {
			var err error
			pid, err = readFileContents(pidPath)
			if err != nil {
				return "", fmt.Errorf("failed to read pid file: %w", err)
			}
			break
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("failed to wait for %s to start before timeout: %w", cmdName, ctx.Err())
		case <-ticker.C:
		}
	}
	return pid, nil
}

func fileExistsAndNotEmpty(filename string) bool {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		fmt.Printf("Error stating file: %v\n", err)
		return false
	}
	return fileInfo.Size() > 0
}

func readFileContents(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
