// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/tests/fixture/bootstrapmonitor"
	"github.com/ava-labs/avalanchego/utils/logging"
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
		rawLogFormat      string
	)
	rootCmd := &cobra.Command{
		Use:   commandName,
		Short: commandName + " commands",
	}
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", os.Getenv("POD_NAMESPACE"), "The namespace of the pod")
	rootCmd.PersistentFlags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the pod")
	rootCmd.PersistentFlags().StringVar(&nodeContainerName, "node-container-name", "", "The name of the node container in the pod")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "The path of the data directory used for the bootstrap job")
	rootCmd.PersistentFlags().StringVar(&rawLogFormat, "log-format", logging.AutoString, logging.FormatDescription)

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version details",
		RunE: func(*cobra.Command, []string) error {
			msg := cliVersion
			if len(version.GitCommit) > 0 {
				msg += ", commit=" + version.GitCommit
			}
			fmt.Fprintln(os.Stdout, msg)
			return nil
		},
	}
	rootCmd.AddCommand(versionCmd)

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new bootstrap test",
		RunE: func(*cobra.Command, []string) error {
			if err := checkArgs(namespace, podName, nodeContainerName, dataDir); err != nil {
				return err
			}
			log, err := newLogger(rawLogFormat)
			if err != nil {
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
			log, err := newLogger(rawLogFormat)
			if err != nil {
				return err
			}
			return bootstrapmonitor.WaitForCompletion(log, namespace, podName, nodeContainerName, dataDir, healthCheckInterval, imageCheckInterval)
		},
	}
	waitCmd.PersistentFlags().DurationVar(&healthCheckInterval, "health-check-interval", defaultHealthCheckInterval, "The interval at which to check for node health")
	waitCmd.PersistentFlags().DurationVar(&imageCheckInterval, "image-check-interval", defaultImageCheckInterval, "The interval at which to check for a new image")
	rootCmd.AddCommand(waitCmd)

	if err := rootCmd.Execute(); err != nil {
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

func newLogger(rawLogFormat string) (logging.Logger, error) {
	writeCloser := os.Stdout
	logFormat, err := logging.ToFormat(rawLogFormat, writeCloser.Fd())
	if err != nil {
		return nil, err
	}
	return logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, writeCloser, logFormat.ConsoleEncoder())), nil
}
