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

	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"
	"github.com/ava-labs/avalanchego/version"
)

const cliVersion = "0.0.1"

var (
	errAvalancheGoRequired = fmt.Errorf("--avalanchego-path or %s are required", local.AvalancheGoPathEnvName)
	errNetworkDirRequired  = fmt.Errorf("--network-dir or %s are required", local.NetworkDirEnvName)
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "testnetctl",
		Short: "testnetctl commands",
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
		rootDir        string
		execPath       string
		nodeCount      uint8
		fundedKeyCount uint8
	)
	startNetworkCmd := &cobra.Command{
		Use:   "start-network",
		Short: "Start a new local network",
		RunE: func(*cobra.Command, []string) error {
			if len(execPath) == 0 {
				return errAvalancheGoRequired
			}

			// Root dir will be defaulted on start if not provided

			network := &local.LocalNetwork{
				LocalConfig: local.LocalConfig{
					ExecPath: execPath,
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), local.DefaultNetworkStartTimeout)
			defer cancel()
			network, err := local.StartNetwork(ctx, os.Stdout, rootDir, network, int(nodeCount), int(fundedKeyCount))
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

			fmt.Fprintf(os.Stdout, "\nConfigure testnetctl to target this network by default with one of the following statements:")
			fmt.Fprintf(os.Stdout, "\n - source %s\n", network.EnvFilePath())
			fmt.Fprintf(os.Stdout, " - %s\n", network.EnvFileContents())
			fmt.Fprintf(os.Stdout, " - export %s=%s\n", local.NetworkDirEnvName, latestSymlinkPath)

			return nil
		},
	}
	startNetworkCmd.PersistentFlags().StringVar(&rootDir, "root-dir", os.Getenv(local.RootDirEnvName), "The path to the root directory for local networks")
	startNetworkCmd.PersistentFlags().StringVar(&execPath, "avalanchego-path", os.Getenv(local.AvalancheGoPathEnvName), "The path to an avalanchego binary")
	startNetworkCmd.PersistentFlags().Uint8Var(&nodeCount, "node-count", testnet.DefaultNodeCount, "Number of nodes the network should initially consist of")
	startNetworkCmd.PersistentFlags().Uint8Var(&fundedKeyCount, "funded-key-count", testnet.DefaultFundedKeyCount, "Number of funded keys the network should start with")
	rootCmd.AddCommand(startNetworkCmd)

	var networkDir string
	stopNetworkCmd := &cobra.Command{
		Use:   "stop-network",
		Short: "Stop a local network",
		RunE: func(*cobra.Command, []string) error {
			if len(networkDir) == 0 {
				return errNetworkDirRequired
			}
			if err := local.StopNetwork(networkDir); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Stopped network configured at: %s\n", networkDir)
			return nil
		},
	}
	stopNetworkCmd.PersistentFlags().StringVar(&networkDir, "network-dir", os.Getenv(local.NetworkDirEnvName), "The path to the configuration directory of a local network")
	rootCmd.AddCommand(stopNetworkCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "testnetctl failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
