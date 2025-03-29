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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

const cliVersion = "0.0.1"

var (
	errAvalancheGoRequired = fmt.Errorf("--avalanchego-path or %s are required", tmpnet.AvalancheGoPathEnvName)
	errNetworkDirRequired  = fmt.Errorf("--network-dir or %s are required", tmpnet.NetworkDirEnvName)
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

	// Vars common to network start and kind cluster start
	var (
		kubeConfigPath    string
		kubeConfigContext string
		kubeNamespace     string
	)

	var (
		rootDir         string
		networkOwner    string
		runtime         string
		avalancheGoPath string
		pluginDir       string
		imageName       string
		nodeCount       uint8
	)
	startNetworkCmd := &cobra.Command{
		Use:   "start-network",
		Short: "Start a new temporary network",
		RunE: func(*cobra.Command, []string) error {
			// TODO(marun) Make all errors constants
			runtimeConfig := tmpnet.NodeRuntimeConfig{}
			switch runtime {
			case "process":
				if len(avalancheGoPath) == 0 {
					return errAvalancheGoRequired
				}
				runtimeConfig.AvalancheGoPath = avalancheGoPath
			case "kube":
				if len(kubeConfigPath) == 0 {
					return errors.New("--kubeconfig is required")
				}
				if len(kubeNamespace) == 0 {
					return errors.New("--namespace is required")
				}
				if len(imageName) == 0 {
					return errors.New("--image-name is required")
				}
				runtimeConfig.KubeRuntimeConfig = &tmpnet.KubeRuntimeConfig{
					Kubeconfig: kubeConfigPath,
					Namespace:  kubeNamespace,
					ImageName:  imageName,
				}
			default:
				return errors.New("invalid runtime")
			}

			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}

			// Root dir will be defaulted on start if not provided

			network := &tmpnet.Network{
				Owner:                networkOwner,
				Nodes:                tmpnet.NewNodesOrPanic(int(nodeCount)),
				DefaultRuntimeConfig: runtimeConfig,
				DefaultFlags:         tmpnet.FlagsMap{},
			}
			network.SetPluginDir(pluginDir)

			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			if err := tmpnet.BootstrapNewNetwork(
				ctx,
				log,
				network,
				rootDir,
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
	// TODO(marun) Enable reuse of flags across tmpnetctl and e2e
	startNetworkCmd.PersistentFlags().StringVar(&rootDir, "root-dir", os.Getenv(tmpnet.RootDirEnvName), "The path to the root directory for temporary networks")
	startNetworkCmd.PersistentFlags().StringVar(&runtime, "runtime", "process", "[optional] the runtime to use to deploy nodes for the network. Valid options are 'process' and 'kube'.")
	startNetworkCmd.PersistentFlags().StringVar(&avalancheGoPath, "avalanchego-path", os.Getenv(tmpnet.AvalancheGoPathEnvName), "The path to an avalanchego binary")
	startNetworkCmd.PersistentFlags().StringVar(
		&pluginDir,
		"plugin-dir",
		tmpnet.GetEnvWithDefault(tmpnet.AvalancheGoPluginDirEnvName, os.ExpandEnv("$HOME/.avalanchego/plugins")),
		"[optional] the dir containing VM plugins",
	)
	SetKubeFlags(startNetworkCmd.PersistentFlags(), &kubeConfigPath, &kubeConfigContext, &kubeNamespace)
	startNetworkCmd.PersistentFlags().StringVar(&imageName, "image-name", "avaplatform/avalanchego:latest", "The name of the docker image to use for creating nodes")
	startNetworkCmd.PersistentFlags().Uint8Var(&nodeCount, "node-count", tmpnet.DefaultNodeCount, "Number of nodes the network should initially consist of")
	startNetworkCmd.PersistentFlags().StringVar(&networkOwner, "network-owner", "", "The string identifying the intended owner of the network")
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
	SetKubeFlags(startKindClusterCmd.PersistentFlags(), &kubeConfigPath, &kubeConfigContext, &kubeNamespace)
	rootCmd.AddCommand(startKindClusterCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "tmpnetctl failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func SetKubeFlags(flagSet *pflag.FlagSet, kubeConfigPath *string, kubeConfigContext *string, namespace *string) {
	flagSet.StringVar(
		kubeConfigPath,
		"kubeconfig",
		tmpnet.GetEnvWithDefault("KUBECONFIG", os.ExpandEnv("$HOME/.kube/config")),
		"The path to a kubernetes configuration file for the target cluster",
	)
	flagSet.StringVar(
		kubeConfigContext,
		"kubeconfig-context",
		"",
		"The path to a kubernetes configuration file for the target cluster",
	)
	flagSet.StringVar(
		namespace,
		"namespace",
		tmpnet.DefaultTmpnetNamespace,
		"The namespace in the target cluster to create nodes in",
	)
}
