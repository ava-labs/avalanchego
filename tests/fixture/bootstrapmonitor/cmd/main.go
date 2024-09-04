// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/tests/fixture/bootstrapmonitor"
	"github.com/ava-labs/avalanchego/version"
)

const (
	cliVersion  = "0.0.1"
	commandName = "bootstrap-monitor"

	defaultPollInterval = 60 * time.Second
)

func main() {
	var namespace string
	var podName string
	var nodeContainerName string
	var dataDir string
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

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the data path of  athe Start a new temporary network",
		RunE: func(*cobra.Command, []string) error {
			if err := checkArgs(namespace, podName, nodeContainerName, dataDir); err != nil {
				return err
			}
			return bootstrapmonitor.InitBootstrapTest(namespace, podName, nodeContainerName, dataDir)
		},
	}
	rootCmd.AddCommand(initCmd)

	var pollInterval time.Duration
	waitCmd := &cobra.Command{
		Use:   "wait-for-completion",
		Short: "Wait for the local node to report healthy indicating completion of bootstrapping",
		RunE: func(*cobra.Command, []string) error {
			if err := checkArgs(namespace, podName, nodeContainerName, dataDir); err != nil {
				return err
			}
			if pollInterval <= 0 {
				return errors.New("poll-interval must be greater than 0")
			}
			return bootstrapmonitor.WaitForCompletion(namespace, podName, nodeContainerName, dataDir, pollInterval)
		},
	}
	waitCmd.PersistentFlags().DurationVar(&pollInterval, "poll-interval", defaultPollInterval, "The interval at which to poll")
	rootCmd.AddCommand(waitCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", commandName, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func checkArgs(namespace string, podName string, nodeContainerName string, dataDir string) error {
	if len(namespace) == 0 {
		return errors.New("namespace is required")
	}
	if len(podName) == 0 {
		return errors.New("pod-name is required")
	}
	if len(nodeContainerName) == 0 {
		return errors.New("node-container-name is required")
	}
	if len(dataDir) == 0 {
		return errors.New("data-dir is required")
	}
	return nil
}
