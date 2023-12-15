// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/version"
)

const cliVersion = "0.0.1"

var (
	errAvalancheGoRequired = fmt.Errorf("--avalanchego-path or %s are required", tmpnet.AvalancheGoPathEnvName)
	errNetworkDirRequired  = fmt.Errorf("--network-dir or %s are required", tmpnet.NetworkDirEnvName)
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tmpnetctl",
		Short: "tmpnetctl commands",
	}

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

	var (
		rootDir           string
		execPath          string
		nodeCount         uint8
		preFundedKeyCount uint8
	)
	startNetworkCmd := &cobra.Command{
		Use:   "start-network",
		Short: "Start a new temporary network",
		RunE: func(*cobra.Command, []string) error {
			if len(execPath) == 0 {
				return errAvalancheGoRequired
			}

			// Root dir will be defaulted on start if not provided

			network := &tmpnet.Network{
				NodeRuntimeConfig: tmpnet.NodeRuntimeConfig{
					ExecPath: execPath,
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkStartTimeout)
			defer cancel()
			network, err := tmpnet.StartNetwork(ctx, os.Stdout, rootDir, network, int(nodeCount), int(preFundedKeyCount))
			if err != nil {
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

			fmt.Fprintf(os.Stdout, "\nConfigure tmpnetctl to target this network by default with one of the following statements:")
			fmt.Fprintf(os.Stdout, "\n - source %s\n", network.EnvFilePath())
			fmt.Fprintf(os.Stdout, " - %s\n", network.EnvFileContents())
			fmt.Fprintf(os.Stdout, " - export %s=%s\n", tmpnet.NetworkDirEnvName, latestSymlinkPath)

			return nil
		},
	}
	startNetworkCmd.PersistentFlags().StringVar(&rootDir, "root-dir", os.Getenv(tmpnet.RootDirEnvName), "The path to the root directory for temporary networks")
	startNetworkCmd.PersistentFlags().StringVar(&execPath, "avalanchego-path", os.Getenv(tmpnet.AvalancheGoPathEnvName), "The path to an avalanchego binary")
	startNetworkCmd.PersistentFlags().Uint8Var(&nodeCount, "node-count", tmpnet.DefaultNodeCount, "Number of nodes the network should initially consist of")
	startNetworkCmd.PersistentFlags().Uint8Var(&preFundedKeyCount, "pre-funded-key-count", tmpnet.DefaultPreFundedKeyCount, "Number of pre-funded keys the network should start with")
	rootCmd.AddCommand(startNetworkCmd)

	var networkDir string
	stopNetworkCmd := &cobra.Command{
		Use:   "stop-network",
		Short: "Stop a temporary network",
		RunE: func(*cobra.Command, []string) error {
			if len(networkDir) == 0 {
				return errNetworkDirRequired
			}
			if err := tmpnet.StopNetwork(networkDir); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Stopped network configured at: %s\n", networkDir)
			return nil
		},
	}
	stopNetworkCmd.PersistentFlags().StringVar(&networkDir, "network-dir", os.Getenv(tmpnet.NetworkDirEnvName), "The path to the configuration directory of a temporary network")
	rootCmd.AddCommand(stopNetworkCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "tmpnetctl failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
