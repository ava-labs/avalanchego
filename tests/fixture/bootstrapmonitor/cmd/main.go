// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/tests/fixture/bootstrapmonitor"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
)

const (
	cliVersion  = "0.0.1"
	commandName = "bootstrap-monitor"

	defaultHealthCheckInterval = 1 * time.Minute
	defaultImageCheckInterval  = 5 * time.Minute
)

func main() {
	var (
		namespace         string
		podName           string
		nodeContainerName string
		dataDir           string
	)
	rootCmd := &cobra.Command{
		Use:   commandName,
		Short: commandName + " commands",
	}
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", os.Getenv("POD_NAMESPACE"), "The namespace of the pod")
	rootCmd.PersistentFlags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the pod")
	rootCmd.PersistentFlags().StringVar(&nodeContainerName, "node-container-name", "", "The name of the node container in the pod")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "The path of the data directory used for the bootstrap job")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version details",
		RunE: func(*cobra.Command, []string) error {
			msg := cliVersion
			if len(version.GitCommit) > 0 {
				msg += ", commit=" + version.GitCommit
			}
			fmt.Fprintf(os.Stdout, msg+"\n")
			return nil
		},
	}
	rootCmd.AddCommand(versionCmd)

	// Use avalanchego logger for consistency
	log := logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, os.Stdout, logging.Plain.ConsoleEncoder()))

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new bootstrap test",
		RunE: func(*cobra.Command, []string) error {
			if err := checkArgs(namespace, podName, nodeContainerName, dataDir); err != nil {
				return err
			}
			return bootstrapmonitor.InitBootstrapTest(log, namespace, podName, nodeContainerName, dataDir)
		},
	}
	rootCmd.AddCommand(initCmd)

	var (
		healthCheckInterval time.Duration
		imageCheckInterval  time.Duration
	)
	waitCmd := &cobra.Command{
		Use:   "wait-for-completion",
		Short: "Wait for the local node to report healthy indicating completion of bootstrapping",
		RunE: func(*cobra.Command, []string) error {
			if err := checkArgs(namespace, podName, nodeContainerName, dataDir); err != nil {
				return err
			}
			if healthCheckInterval <= 0 {
				return errors.New("--health-check-interval must be greater than 0")
			}
			if imageCheckInterval <= 0 {
				return errors.New("--image-check-interval must be greater than 0")
			}
			return bootstrapmonitor.WaitForCompletion(log, namespace, podName, nodeContainerName, dataDir, healthCheckInterval, imageCheckInterval)
		},
	}
	waitCmd.PersistentFlags().DurationVar(&healthCheckInterval, "health-check-interval", defaultHealthCheckInterval, "The interval at which to check for node health")
	waitCmd.PersistentFlags().DurationVar(&imageCheckInterval, "image-check-interval", defaultImageCheckInterval, "The interval at which to check for a new image")
	rootCmd.AddCommand(waitCmd)

	var stateSyncEnabled bool
	chainConfigCmd := &cobra.Command{
		Use:   "chain-config",
		Short: "Output recommended base64-encoded chain configuration for use with --chain-config-content",
		RunE: func(*cobra.Command, []string) error {
			chainConfig, err := encodedChainConfig(stateSyncEnabled)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, chainConfig+"\n")
			return nil
		},
	}
	chainConfigCmd.PersistentFlags().BoolVar(&stateSyncEnabled, "state-sync-enabled", true, "Whether the configuration will enable state sync")
	rootCmd.AddCommand(chainConfigCmd)

	dbConfigCmd := &cobra.Command{
		Use:   "db-config",
		Short: "Output recommended base64-encoded db configuration for use with --db-config-file-content",
		RunE: func(*cobra.Command, []string) error {
			dbConfig, err := encodedDBConfig()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, dbConfig+"\n")
			return nil
		},
	}
	rootCmd.AddCommand(dbConfigCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Error(commandName+" failed", zap.Error(err))
		os.Exit(1)
	}
	os.Exit(0)
}

func checkArgs(namespace string, podName string, nodeContainerName string, dataDir string) error {
	if len(namespace) == 0 {
		return errors.New("--namespace is required")
	}
	if len(podName) == 0 {
		return errors.New("--pod-name is required")
	}
	if len(nodeContainerName) == 0 {
		return errors.New("--node-container-name is required")
	}
	if len(dataDir) == 0 {
		return errors.New("--data-dir is required")
	}
	return nil
}

func encodedChainConfig(stateSyncEnabled bool) (string, error) {
	flags := map[string]map[string]any{
		"C": {
			"state-sync-enabled": stateSyncEnabled,
			"trie-clean-cache":   4096,
			"trie-dirty-cache":   4096,
		},
	}
	chainConfigs := map[string]chains.ChainConfig{}
	for chainID, chainFlags := range flags {
		marshaledFlags, err := json.Marshal(chainFlags)
		if err != nil {
			return "", fmt.Errorf("failed to marshal flags map for %s: %w", chainID, err)
		}
		chainConfigs[chainID] = chains.ChainConfig{
			Config: marshaledFlags,
		}
	}
	marshaledChainConfigs, err := json.Marshal(chainConfigs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chain configs: %w", err)
	}
	return base64.StdEncoding.EncodeToString(marshaledChainConfigs), nil
}

func encodedDBConfig() (string, error) {
	dbFlags := map[string]any{
		"blockCacheCapacity": 3 * units.GiB / (2 * units.MiB),
	}
	marshaledDBFlags, err := json.Marshal(dbFlags)
	if err != nil {
		return "", fmt.Errorf("failed to marshal db flags: %w", err)
	}
	return base64.StdEncoding.EncodeToString(marshaledDBFlags), nil
}
