// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

const cliVersion = "0.0.1"

var errNetworkDirRequired = fmt.Errorf("--network-dir or %s are required", tmpnet.NetworkDirEnvName)

func main() {
	var (
		networkDir   string
		rawLogFormat string
	)
	rootCmd := &cobra.Command{
		Use:   "tmpnetctl",
		Short: "tmpnetctl commands",
	}
	rootCmd.PersistentFlags().StringVar(&networkDir, "network-dir", os.Getenv(tmpnet.NetworkDirEnvName), "The path to the configuration directory of a temporary network")
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

	var startNetworkVars *flags.StartNetworkVars
	startNetworkCmd := &cobra.Command{
		Use:   "start-network",
		Short: "Start a new temporary network",
		RunE: func(*cobra.Command, []string) error {
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}

			nodeCount, err := startNetworkVars.GetNodeCount()
			if err != nil {
				return err
			}

			nodeRuntimeConfig, err := startNetworkVars.GetNodeRuntimeConfig()
			if err != nil {
				return err
			}

			network := &tmpnet.Network{
				Owner:                startNetworkVars.NetworkOwner,
				Nodes:                tmpnet.NewNodesOrPanic(nodeCount),
				DefaultRuntimeConfig: *nodeRuntimeConfig,
			}

			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			if err := tmpnet.BootstrapNewNetwork(
				ctx,
				log,
				network,
				startNetworkVars.RootNetworkDir,
			); err != nil {
				log.Error("failed to bootstrap network", zap.Error(err))
				return err
			}

			// Symlink the new network to the 'latest' network to simplify usage
			networkRootDir := filepath.Dir(network.Dir)
			networkDirName := filepath.Base(network.Dir)
			latestSymlinkPath := filepath.Join(networkRootDir, "latest")
			if err := os.Remove(latestSymlinkPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := os.Symlink(networkDirName, latestSymlinkPath); err != nil {
				return err
			}

			fmt.Fprintln(os.Stdout, "\nConfigure tmpnetctl to target this network by default with one of the following statements:")
			fmt.Fprintf(os.Stdout, " - source %s\n", network.EnvFilePath())
			fmt.Fprintf(os.Stdout, " - %s\n", network.EnvFileContents())
			fmt.Fprintf(os.Stdout, " - export %s=%s\n", tmpnet.NetworkDirEnvName, latestSymlinkPath)

			return nil
		},
	}
	startNetworkVars = flags.NewStartNetworkFlagSetVars(startNetworkCmd.PersistentFlags(), "")
	rootCmd.AddCommand(startNetworkCmd)

	stopNetworkCmd := &cobra.Command{
		Use:   "stop-network",
		Short: "Stop a temporary network",
		RunE: func(*cobra.Command, []string) error {
			if len(networkDir) == 0 {
				return errNetworkDirRequired
			}
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			if err := tmpnet.StopNetwork(ctx, networkDir); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Stopped network configured at: %s\n", networkDir)
			return nil
		},
	}
	rootCmd.AddCommand(stopNetworkCmd)

	restartNetworkCmd := &cobra.Command{
		Use:   "restart-network",
		Short: "Restart a temporary network",
		RunE: func(*cobra.Command, []string) error {
			if len(networkDir) == 0 {
				return errNetworkDirRequired
			}
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			return tmpnet.RestartNetwork(ctx, log, networkDir)
		},
	}
	rootCmd.AddCommand(restartNetworkCmd)

	startCollectorsCmd := &cobra.Command{
		Use:   "start-collectors",
		Short: "Start log and metric collectors for local process-based nodes",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.StartCollectors(ctx, log)
		},
	}
	rootCmd.AddCommand(startCollectorsCmd)

	stopCollectorsCmd := &cobra.Command{
		Use:   "stop-collectors",
		Short: "Stop log and metric collectors for local process-based nodes",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.StopCollectors(ctx, log)
		},
	}
	rootCmd.AddCommand(stopCollectorsCmd)

	var networkUUID string

	checkMetricsCmd := &cobra.Command{
		Use:   "check-metrics",
		Short: "Checks whether the default prometheus server has the expected metrics",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.CheckMetricsExist(ctx, log, networkUUID)
		},
	}
	checkMetricsCmd.PersistentFlags().StringVar(
		&networkUUID,
		"network-uuid",
		"",
		"[optional] The network UUID to check metrics for. Labels read from GH_* env vars will always be used.",
	)
	rootCmd.AddCommand(checkMetricsCmd)

	checkLogsCmd := &cobra.Command{
		Use:   "check-logs",
		Short: "Checks whether the default loki server has the expected logs",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.CheckLogsExist(ctx, log, networkUUID)
		},
	}
	checkLogsCmd.PersistentFlags().StringVar(
		&networkUUID,
		"network-uuid",
		"",
		"[optional] The network UUID to check logs for. Labels read from GH_* env vars will always be used.",
	)
	rootCmd.AddCommand(checkLogsCmd)

	var (
		kubeConfigPath    string
		kubeConfigContext string
	)
	startKindClusterCmd := &cobra.Command{
		Use:   "start-kind-cluster",
		Short: "Starts a local kind cluster with an integrated registry",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.StartKindCluster(ctx, log, kubeConfigPath, kubeConfigContext)
		},
	}
	SetKubeConfigFlags(startKindClusterCmd.PersistentFlags(), &kubeConfigPath, &kubeConfigContext)
	rootCmd.AddCommand(startKindClusterCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "tmpnetctl failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func SetKubeConfigFlags(flagSet *pflag.FlagSet, kubeConfigPath *string, kubeConfigContext *string) {
	flagSet.StringVar(
		kubeConfigPath,
		"kubeconfig",
		os.Getenv("KUBECONFIG"),
		"The path to a kubernetes configuration file for the target cluster",
	)
	flagSet.StringVar(
		kubeConfigContext,
		"kubeconfig-context",
		"",
		"The path to a kubernetes configuration file for the target cluster",
	)
}
