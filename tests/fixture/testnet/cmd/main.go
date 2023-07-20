// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"
)

func main() {
	var networkDir string
	var rootDir string
	rootCmd := &cobra.Command{
		Use:   "testnetctl",
		Short: "testnetctl commands",
	}
	rootCmd.PersistentFlags().StringVar(&networkDir, "network-dir", os.Getenv(local.NetworkDirEnvName), "The path to the configuration directory of a local network")
	rootCmd.PersistentFlags().StringVar(&rootDir, "root-dir", os.Getenv(local.RootDirEnvName), "The path to the root directory for local networks")

	var execPath string
	var nodeCount uint16
	var useStaticPorts bool
	var initialStaticPort uint16
	startNetworkCmd := &cobra.Command{
		Use:   "start-network [/path/to/avalanchego]",
		Short: "Start a new local network",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(execPath) == 0 {
				return fmt.Errorf("--avalanchego-path or %s are required", local.AvalancheGoEnvName)
			}

			// Root dir will be defaulted on start if not provided

			network := &local.LocalNetwork{
				LocalConfig: local.LocalConfig{
					ExecPath:          execPath,
					UseStaticPorts:    useStaticPorts,
					InitialStaticPort: initialStaticPort,
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			network, err := local.StartNetwork(ctx, os.Stdout, rootDir, network, 5, 50)
			if err != nil {
				return err
			}

			// Symlink the new network to the 'latest' network to simplify usage
			networkRootDir := filepath.Dir(network.Dir)
			networkDirName := filepath.Base(network.Dir)
			latestSymlinkPath := filepath.Join(networkRootDir, "latest")
			if err := os.Remove(latestSymlinkPath); err != nil && !os.IsNotExist(err) {
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
	startNetworkCmd.PersistentFlags().StringVar(&execPath, "avalanchego-path", os.Getenv(local.AvalancheGoEnvName), "The path to an avalanchego binary")
	startNetworkCmd.PersistentFlags().Uint16Var(&nodeCount, "node-count", 5, "Number of nodes the network should initially consist of")
	startNetworkCmd.PersistentFlags().BoolVar(&useStaticPorts, "use-static-ports", false, "Whether to attempt to configure nodes with static ports. A network will start faster using statically assigned ports but start will fail if the ports chosen are already bound.")
	// The default initial static port should differ from the ANR default of 9650.
	startNetworkCmd.PersistentFlags().Uint16Var(&initialStaticPort, "initial-static-port", 9850, "The initial port number from which API and staking ports will be statically determined.")

	rootCmd.AddCommand(startNetworkCmd)

	stopNetworkCmd := &cobra.Command{
		Use:   "stop-network",
		Short: "Stop a local network",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(networkDir) == 0 {
				return fmt.Errorf("--network-dir or %s are required", local.NetworkDirEnvName)
			}
			if err := local.StopNetwork(networkDir); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Stopped network configured at: %s\n", networkDir)
			return nil
		},
	}
	rootCmd.AddCommand(stopNetworkCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "testnetctl failed %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
