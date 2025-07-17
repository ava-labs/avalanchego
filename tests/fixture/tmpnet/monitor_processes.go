// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	collectorTickerInterval = 100 * time.Millisecond

	// TODO(marun) Maybe use dynamic HTTP ports to avoid the possibility of them being already bound?

	// Prometheus configuration
	prometheusCmd            = "prometheus"
	defaultPrometheusURL     = "https://prometheus-poc.avax-dev.network"
	prometheusScrapeInterval = 10 * time.Second
	prometheusListenAddress  = "127.0.0.1:9090"
	prometheusReadinessURL   = "http://" + prometheusListenAddress + "/-/ready"

	// Promtail configuration
	promtailCmd          = "promtail"
	defaultLokiURL       = "https://loki-poc.avax-dev.network"
	promtailHTTPPort     = "3101"
	promtailReadinessURL = "http://127.0.0.1:" + promtailHTTPPort + "/ready"

	// Use a delay slightly longer than the scrape interval to ensure a final scrape before shutdown
	NetworkShutdownDelay = prometheusScrapeInterval + 2*time.Second
)

// StartPrometheus ensures prometheus is running to collect metrics from local nodes.
func StartPrometheus(ctx context.Context, log logging.Logger) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("unable to start prometheus with a context without a deadline")
	}
	if err := startPrometheus(ctx, log); err != nil {
		return err
	}
	if err := waitForReadiness(ctx, log, prometheusCmd, prometheusReadinessURL); err != nil {
		return err
	}
	log.Info("To stop: tmpnetctl stop-metrics-collector")
	return nil
}

// StartPromtail ensures promtail is running to collect logs from local nodes.
func StartPromtail(ctx context.Context, log logging.Logger) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("unable to start promtail with a context without a deadline")
	}
	if err := startPromtail(ctx, log); err != nil {
		return err
	}
	log.Info("skipping promtail readiness check until one or more nodes have written their service discovery configuration")
	log.Info("To stop: tmpnetctl stop-logs-collector")
	return nil
}

// WaitForPromtailReadiness waits until prometheus is ready. It can only succeed after
// one or more nodes have written their service discovery configuration.
func WaitForPromtailReadiness(ctx context.Context, log logging.Logger) error {
	return waitForReadiness(ctx, log, promtailCmd, promtailReadinessURL)
}

// StopMetricsCollector ensures prometheus is not running.
func StopMetricsCollector(ctx context.Context, log logging.Logger) error {
	return stopCollector(ctx, log, prometheusCmd)
}

// StopLogsCollector ensures promtail is not running.
func StopLogsCollector(ctx context.Context, log logging.Logger) error {
	return stopCollector(ctx, log, promtailCmd)
}

// stopCollector stops the collector process if it is running.
func stopCollector(ctx context.Context, log logging.Logger, cmdName string) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("unable to start collectors with a context without a deadline")
	}

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
		return nil
	}

	log.Info("sending SIGTERM to collector process",
		zap.String("cmd", cmdName),
		zap.Int("pid", proc.Pid),
	)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to pid %d: %w", proc.Pid, err)
	}

	log.Info("waiting for collector process to stop",
		zap.String("cmd", cmdName),
		zap.Int("pid", proc.Pid),
	)
	err = pollUntilContextCancel(
		ctx,
		func(_ context.Context) (bool, error) {
			p, err := getProcess(proc.Pid)
			if err != nil {
				return false, fmt.Errorf("failed to retrieve process: %w", err)
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
			}
			return p == nil, nil
		},
	)
	if err != nil {
		return err
	}
	log.Info("collector stopped",
		zap.String("cmdName", cmdName),
	)

	return nil
}

// startPrometheus ensures an agent-mode prometheus process is running to collect metrics from local nodes.
func startPrometheus(ctx context.Context, log logging.Logger) error {
	cmdName := prometheusCmd

	args := fmt.Sprintf(
		"--config.file=%s.yaml --web.listen-address=%s --agent --storage.agent.path=./data",
		cmdName,
		prometheusListenAddress,
	)

	username, password, err := getCollectorCredentials(cmdName)
	if err != nil {
		return err
	}

	serviceDiscoveryDir, err := getServiceDiscoveryDir(cmdName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(serviceDiscoveryDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create service discovery dir: %w", err)
	}

	config := fmt.Sprintf(`
global:
  scrape_interval: %v     # Default is every 1 minute.
  evaluation_interval: 10s # The default is every 1 minute.
  scrape_timeout: 5s       # The default is every 10s

scrape_configs:
  - job_name: "avalanchego"
    metrics_path: "/ext/metrics"
    file_sd_configs:
      - files:
          - '%s/*.json'

remote_write:
  - url: "%s/api/v1/write"
    basic_auth:
      username: "%s"
      password: "%s"
`, prometheusScrapeInterval, serviceDiscoveryDir, getPrometheusURL(), username, password)

	return startCollector(ctx, log, cmdName, args, config)
}

// startPromtail ensures a promtail process is running to collect logs from local nodes.
func startPromtail(ctx context.Context, log logging.Logger) error {
	cmdName := promtailCmd

	args := fmt.Sprintf("-config.file=%s.yaml", cmdName)

	username, password, err := getCollectorCredentials(cmdName)
	if err != nil {
		return err
	}

	workingDir, err := getWorkingDir(cmdName)
	if err != nil {
		return err
	}

	serviceDiscoveryDir, err := getServiceDiscoveryDir(cmdName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(serviceDiscoveryDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create service discovery dir: %w", err)
	}

	config := fmt.Sprintf(`
server:
  http_listen_port: %s
  grpc_listen_port: 0

positions:
  filename: %s/positions.yaml

client:
  url: "%s/api/prom/push"
  basic_auth:
    username: "%s"
    password: "%s"

scrape_configs:
  - job_name: "avalanchego"
    file_sd_configs:
      - files:
          - '%s/*.json'
`, promtailHTTPPort, workingDir, getLokiURL(), username, password, serviceDiscoveryDir)

	return startCollector(ctx, log, cmdName, args, config)
}

func getWorkingDir(cmdName string) (string, error) {
	tmpnetDir, err := getTmpnetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpnetDir, cmdName), nil
}

// GetPrometheusServiceDiscoveryDir returns the path for prometheus file-based
// service discovery configuration.
func GetPrometheusServiceDiscoveryDir() (string, error) {
	return getServiceDiscoveryDir(prometheusCmd)
}

func getServiceDiscoveryDir(cmdName string) (string, error) {
	tmpnetDir, err := getTmpnetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpnetDir, cmdName, "file_sd_configs"), nil
}

// SDConfig represents a Prometheus service discovery config entry.
//
// file_sd_config docs: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config
type SDConfig struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

// WritePrometheusSDConfig writes the SDConfig with the provided name
// to the location expected by the prometheus instance start by tmpnet.
//
// If withGitHubLabels is true, checks env vars for GitHub-specific labels
// and adds them as labels if present before writing the SDConfig.
//
// Returns the path to the written configuration file.
func WritePrometheusSDConfig(name string, sdConfig SDConfig, withGitHubLabels bool) (string, error) {
	serviceDiscoveryDir, err := GetPrometheusServiceDiscoveryDir()
	if err != nil {
		return "", fmt.Errorf("failed to get service discovery dir: %w", err)
	}

	if err := os.MkdirAll(serviceDiscoveryDir, perms.ReadWriteExecute); err != nil {
		return "", fmt.Errorf("failed to create service discovery dir: %w", err)
	}

	if withGitHubLabels {
		sdConfig = applyGitHubLabels(sdConfig)
	}

	configPath := filepath.Join(serviceDiscoveryDir, name+".json")
	configData, err := DefaultJSONMarshal([]SDConfig{sdConfig})
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, perms.ReadWrite); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}

	return configPath, nil
}

func applyGitHubLabels(sdConfig SDConfig) SDConfig {
	for label, value := range GetGitHubLabels() {
		sdConfig.Labels[label] = value
	}
	return sdConfig
}

func getLogFilename(cmdName string) string {
	return cmdName + ".log"
}

func getLogPath(cmdName string) (string, error) {
	tmpnetDir, err := getTmpnetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpnetDir, cmdName, getLogFilename(cmdName)), nil
}

func getPIDPath(workingDir string) string {
	return filepath.Join(workingDir, "run.pid")
}

// startCollector starts a collector process if it is not already running.
func startCollector(
	ctx context.Context,
	log logging.Logger,
	cmdName string,
	args string,
	config string,
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
		log.Info("collector already running",
			zap.String("cmd", cmdName),
		)
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
	confFilename := cmdName + ".yaml"
	confPath := filepath.Join(workingDir, confFilename)
	log.Info("writing collector config",
		zap.String("cmd", cmdName),
		zap.String("path", confPath),
	)
	if err := os.WriteFile(confPath, []byte(config), perms.ReadWrite); err != nil {
		return err
	}

	// Start the process
	return startCollectorProcess(ctx, log, cmdName, args, workingDir, pidPath)
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
		log.Info("deleted stale collector pid file",
			zap.String("cmd", cmdName),
			zap.String("path", pidPath),
		)
	}
	return nil
}

func getPrometheusURL() string {
	return GetEnvWithDefault("PROMETHEUS_URL", defaultPrometheusURL)
}

func getLokiURL() string {
	return GetEnvWithDefault("LOKI_URL", defaultLokiURL)
}

// getCollectorCredentials retrieves the username and password for the command.
func getCollectorCredentials(cmdName string) (string, string, error) {
	var baseEnvName string
	switch cmdName {
	case prometheusCmd:
		baseEnvName = "PROMETHEUS"
	case promtailCmd:
		baseEnvName = "LOKI"
	default:
		return "", "", fmt.Errorf("unsupported cmd: %s", cmdName)
	}

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

// Start a collector process. Use bash to execute the command in the background and enable
// stderr and stdout redirection to a log file.
//
// Ideally this would be possible without bash, but it does not seem possible to
// have this process open a log file, set cmd.Stdout cmd.Stderr to that file, and
// then have the child process be able to write to that file once the parent
// process exits. Attempting to do so resulted in an empty log file.
func startCollectorProcess(
	ctx context.Context,
	log logging.Logger,
	cmdName string,
	args string,
	workingDir string,
	pidPath string,
) error {
	logFilename := getLogFilename(cmdName)
	fullCmd := "nohup " + cmdName + " " + args + " > " + logFilename + " 2>&1 & echo -n \"$!\" > " + pidPath
	log.Info("starting collector",
		zap.String("cmd", cmdName),
		zap.String("workingDir", workingDir),
		zap.String("fullCmd", fullCmd),
		zap.String("logPath", filepath.Join(workingDir, logFilename)),
	)

	cmd := exec.Command("bash", "-c", fullCmd)
	configureDetachedProcess(cmd) // Ensure the child process will outlive its parent
	cmd.Dir = workingDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", cmdName, err)
	}

	// Wait for PID file
	var pid int
	err := pollUntilContextCancel(
		ctx,
		func(_ context.Context) (bool, error) {
			var err error
			pid, err = getPID(cmdName, pidPath)
			if err != nil {
				log.Warn("failed to read PID file",
					zap.String("cmd", cmdName),
					zap.String("pidPath", pidPath),
					zap.Error(err),
				)
			}
			return pid != 0, nil
		},
	)
	if err != nil {
		return err
	}
	log.Info("started collector",
		zap.String("cmd", cmdName),
		zap.Int("pid", pid),
	)

	// Wait for non-empty log file. An empty log file should only occur if the command
	// invocation is not correctly redirecting stderr and stdout to the expected file.
	logPath := filepath.Join(workingDir, logFilename)
	err = pollUntilContextCancel(
		ctx,
		func(_ context.Context) (bool, error) {
			logData, err := os.ReadFile(logPath)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return false, fmt.Errorf("failed to read log file %s for %s: %w", logPath, cmdName, err)
			}
			return len(logData) != 0, nil
		},
	)
	if err != nil {
		return fmt.Errorf("empty log file %s for %s indicates misconfiguration: %w", logPath, cmdName, err)
	}

	return nil
}

// checkReadiness retrieves the provided URL and indicates whether it returned 200
func checkReadiness(ctx context.Context, url string) (bool, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("failed to read response: %w", err)
	}

	return resp.StatusCode == http.StatusOK, string(body), nil
}

// waitForReadiness waits until the given readiness URL returns 200
func waitForReadiness(ctx context.Context, log logging.Logger, cmdName string, readinessURL string) error {
	logPath, err := getLogPath(cmdName)
	if err != nil {
		return err
	}
	log.Info("waiting for collector readiness",
		zap.String("cmd", cmdName),
		zap.String("url", readinessURL),
		zap.String("logPath", logPath),
	)
	err = pollUntilContextCancel(
		ctx,
		func(_ context.Context) (bool, error) {
			ready, body, err := checkReadiness(ctx, readinessURL)
			if err == nil {
				return ready, nil
			}
			log.Warn("failed to check readiness",
				zap.String("cmd", cmdName),
				zap.String("url", readinessURL),
				zap.String("body", body),
				zap.Error(err),
			)
			return false, nil
		},
	)
	if err != nil {
		return err
	}
	log.Info("collector ready",
		zap.String("cmd", cmdName),
	)
	return nil
}

func pollUntilContextCancel(ctx context.Context, condition wait.ConditionWithContextFunc) error {
	return wait.PollUntilContextCancel(ctx, collectorTickerInterval, true /* immediate */, condition)
}
