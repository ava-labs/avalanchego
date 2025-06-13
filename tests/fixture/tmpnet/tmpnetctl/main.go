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
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

const cliVersion = "0.0.1"

var (
	errNetworkDirRequired = fmt.Errorf("--network-dir or %s is required", tmpnet.NetworkDirEnvName)
	errKubeconfigRequired = errors.New("--kubeconfig is required")
)

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

			timeout, err := nodeRuntimeConfig.GetNetworkStartTimeout(nodeCount)
			if err != nil {
				return err
			}
			log.Info("waiting for network to start",
				zap.Float64("timeoutSeconds", timeout.Seconds()),
			)

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	startNetworkVars = flags.NewStartNetworkFlagSetVars(startNetworkCmd.PersistentFlags(), "" /* defaultNetworkOwner */)
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
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			if err := tmpnet.StopNetwork(ctx, log, networkDir); err != nil {
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

	startMetricsCollectorCmd := &cobra.Command{
		Use:   "start-metrics-collector",
		Short: "Start metrics collector for local process-based nodes",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.StartPrometheus(ctx, log)
		},
	}
	rootCmd.AddCommand(startMetricsCollectorCmd)

	startLogsCollectorCmd := &cobra.Command{
		Use:   "start-logs-collector",
		Short: "Start logs collector for local process-based nodes",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.StartPromtail(ctx, log)
		},
	}
	rootCmd.AddCommand(startLogsCollectorCmd)

	stopMetricsCollectorCmd := &cobra.Command{
		Use:   "stop-metrics-collector",
		Short: "Stop metrics collector for local process-based nodes",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.StopMetricsCollector(ctx, log)
		},
	}
	rootCmd.AddCommand(stopMetricsCollectorCmd)

	stopLogsCollectorCmd := &cobra.Command{
		Use:   "stop-logs-collector",
		Short: "Stop logs collector for local process-based nodes",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}
			return tmpnet.StopLogsCollector(ctx, log)
		},
	}
	rootCmd.AddCommand(stopLogsCollectorCmd)

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
		kubeconfigVars *flags.KubeconfigVars
		collectorVars  *flags.CollectorVars
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
			// A valid kubeconfig is required for local kind usage but this is not validated by KubeconfigVars
			// since unlike kind, tmpnet usage may involve an implicit in-cluster config.
			if len(kubeconfigVars.Path) == 0 {
				return errKubeconfigRequired
			}
			// TODO(marun) Consider supporting other contexts. Will require modifying the kind cluster start script.
			if len(kubeconfigVars.Context) > 0 && kubeconfigVars.Context != tmpnet.KindKubeconfigContext {
				log.Warn("ignoring kubeconfig context for kind cluster",
					zap.String("providedContext", kubeconfigVars.Context),
					zap.String("requiredContext", tmpnet.KindKubeconfigContext),
				)
			}
			return tmpnet.StartKindCluster(
				ctx,
				log,
				kubeconfigVars.Path,
				collectorVars.StartMetricsCollector,
				collectorVars.StartLogsCollector,
			)
		},
	}
	kubeconfigVars = flags.NewKubeconfigFlagSetVars(startKindClusterCmd.PersistentFlags())
	collectorVars = flags.NewCollectorFlagSetVars(startKindClusterCmd.PersistentFlags())
	rootCmd.AddCommand(startKindClusterCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "tmpnetctl failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
