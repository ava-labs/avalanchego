// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

type configGeneratorFunc func(workingDir string, username string, password string) string

// Use a delay slightly longer than the 10s scrape interval configured for prometheus to ensure a final scrape before shutdown
const NetworkShutdownDelay = 12 * time.Second

func EnsureCollectorsRunning(log logging.Logger) error {
	if err := ensurePrometheusRunning(log); err != nil {
		return err
	}
	return ensurePromtailRunning(log)
}

func ensurePrometheusRunning(log logging.Logger) error {
	return ensureCollectorRunning(
		log,
		"prometheus",
		"--config.file=prometheus.yaml --storage.agent.path=./data --web.listen-address=localhost:0 --enable-feature=agent",
		"PROMETHEUS",
		func(workingDir string, username string, password string) string {
			return fmt.Sprintf(`
global:
  scrape_interval: 10s     # Default is every 1 minute.
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
`, workingDir, username, password)
		},
	)
}

func ensurePromtailRunning(log logging.Logger) error {
	return ensureCollectorRunning(
		log,
		"promtail",
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

func ensureCollectorRunning(
	log logging.Logger,
	cmdName string,
	args string,
	baseEnvName string,
	configGenerator configGeneratorFunc,
) error {
	tmpnetDir, err := getTmpnetPath()
	if err != nil {
		return err
	}
	workingDir := filepath.Join(tmpnetDir, cmdName)
	pidFilename := "run.pid"
	pidPath := filepath.Join(workingDir, pidFilename)

	if err := os.MkdirAll(workingDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create %s dir: %w", cmdName, err)
	}

	if err := os.MkdirAll(filepath.Join(workingDir, "file_sd_configs"), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create promtail file_sd_configs dir: %w", err)
	}

	// Read the PID from the file
	pidData, err := os.ReadFile(pidPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s PID file %s: %w", cmdName, pidPath, err)
	}
	if len(pidData) > 0 {
		pid, err := strconv.Atoi(string(pidData))
		if err != nil {
			return fmt.Errorf("failed to parse %s PID: %w", cmdName, err)
		}
		process, err := getProcess(pid)
		if err != nil {
			return err
		}
		if process != nil {
			log.Info(cmdName + " is already running")
			return nil
		}
	}

	// Remove the pid file to avoid conflicting with the new one starting
	if err := os.Remove(pidPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to remove stale pid file: %w", err)
		}
	} else {
		log.Info("deleted stale "+cmdName+" pid file",
			zap.String("path", pidPath),
		)
	}

	// TODO(marun) Maybe collect errors instead of returning them 1-by-1?
	if _, err := exec.LookPath(cmdName); err != nil {
		return fmt.Errorf("%s command not found. Maybe run 'nix develop'?", cmdName)
	}

	usernameEnvVar := baseEnvName + "_USERNAME"
	username := getEnv(usernameEnvVar, "")
	if len(username) == 0 {
		return fmt.Errorf("%s env var not set", usernameEnvVar)
	}

	passwordEnvVar := baseEnvName + "_PASSWORD"
	password := getEnv(passwordEnvVar, "")
	if len(password) == 0 {
		return fmt.Errorf("%s var not set", passwordEnvVar)
	}

	confFilename := cmdName + ".yaml"
	confPath := filepath.Join(workingDir, confFilename)
	log.Info("writing "+cmdName+" config",
		zap.String("path", confPath),
	)
	config := configGenerator(workingDir, username, password)
	if err := os.WriteFile(confPath, []byte(config), perms.ReadWrite); err != nil {
		return err
	}

	fullCmd := "nohup " + cmdName + " " + args + " > " + cmdName + ".log 2>&1 & echo -n \"$!\" > " + pidFilename
	log.Info("starting "+cmdName,
		zap.String("workingDir", workingDir),
		zap.String("fullCmd", fullCmd),
	)

	// TODO(marun) Figure out a way to redirect stdout and stderr of a detached child process without a bash shell
	cmd := exec.Command("bash", "-c", fullCmd)
	configureDetachedProcess(cmd) // Ensure the child process will outlive its parent
	cmd.Dir = workingDir

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", cmdName, err)
	}

	var pid string
	// TODO(marun) Use a context instead
	for {
		if fileExistsAndNotEmpty(pidPath) {
			var err error
			pid, err = readFileContents(pidPath)
			if err != nil {
				return fmt.Errorf("failed to read pid file: %w", err)
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Info(cmdName+" started",
		zap.String("pid", pid),
	)

	killMsg := fmt.Sprintf("To stop %s: kill -SIGTERM $(cat %s) && rm %s", cmdName, pidPath, pidPath)
	log.Info(killMsg)

	return nil
}

// Function to check if a file exists and is not empty
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

// Function to read the contents of a file
func readFileContents(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// TODO(marun) Put this somewhere standard
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
