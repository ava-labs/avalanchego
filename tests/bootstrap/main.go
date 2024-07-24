// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Simple test that starts a single node and waits for it to finish bootstrapping.

func main() {
	avalanchegoPath := flag.String("avalanchego-path", "", "The path to an avalanchego binary")
	networkID := flag.Int64("network-id", 0, "The ID of the network to bootstrap from")
	stateSyncEnabled := flag.Bool("state-sync-enabled", false, "Whether state syncing should be enabled")
	maxDuration := flag.Duration("max-duration", time.Hour*72, "The maximum duration the network should run for")
	dataDir := flag.String("data-dir", "", "The directory to store the node's data")
	useDynamicPorts := flag.Bool("use-dynamic-ports", false, "Whether the bootstrapping node should bind to dynamic ports")
	wipeDataDir := flag.Bool("wipe-data-dir", false, "Whether to wipe the data dir in preparation for testing")

	flag.Parse()

	if len(*avalanchegoPath) == 0 {
		log.Fatal("avalanchego-path is required")
	}
	if *networkID == 0 {
		log.Fatal("network-id is required")
	}
	if *maxDuration == 0 {
		log.Fatal("max-duration is required")
	}
	if len(*dataDir) == 0 && *wipeDataDir {
		log.Fatal("unable to wipe the data dir when a path is not provided")
	}

	if err := checkBootstrap(*dataDir, *avalanchegoPath, uint32(*networkID), *useDynamicPorts, *wipeDataDir, *stateSyncEnabled, *maxDuration); err != nil {
		log.Fatalf("Failed to check bootstrap: %v\n", err)
	}
}

func checkBootstrap(
	dataDir string,
	avalanchegoPath string,
	networkID uint32,
	useDynamicPorts bool,
	wipeDataDir bool,
	stateSyncEnabled bool,
	maxDuration time.Duration,
) error {
	flags := tmpnet.DefaultLocalhostFlags()
	flags.SetDefaults(tmpnet.FlagsMap{
		config.HealthCheckFreqKey: "30s",
		// Minimize logging overhead
		config.LogDisplayLevelKey: logging.Off.String(),
	})
	if !useDynamicPorts {
		flags.SetDefaults(tmpnet.FlagsMap{
			config.HTTPPortKey:    config.DefaultHTTPPort,
			config.StakingPortKey: config.DefaultStakingPort,
		})
	}

	if wipeDataDir && len(dataDir) > 0 {
		if err := os.RemoveAll(dataDir); err != nil {
			return fmt.Errorf("failed to remove data dir: %w", err)
		}
	}

	networkName := constants.NetworkName(networkID)
	syncString := "full-sync"
	if stateSyncEnabled {
		syncString = "state-sync"
	}

	// Create a new single-node network that will bootstrap from the specified network
	network := &tmpnet.Network{
		UUID:         uuid.NewString(),
		NetworkID:    networkID,
		Owner:        fmt.Sprintf("bootstrap-test-%s-%s", networkName, syncString),
		Nodes:        tmpnet.NewNodesOrPanic(1),
		DefaultFlags: flags,
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			// TODO(marun) Rename AvalancheGoPath to AvalanchegoPath
			AvalancheGoPath: avalanchegoPath,
		},
		ChainConfigs: map[string]tmpnet.FlagsMap{
			"C": {
				"state-sync-enabled": stateSyncEnabled,
			},
		},
	}

	if err := network.Create(dataDir); err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	node := network.Nodes[0]

	log.Printf("Starting node in path %s (UUID: %s)\n", network.Dir, network.UUID)

	ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
	defer cancel()
	if err := network.StartNode(ctx, os.Stdout, node); err != nil {
		return fmt.Errorf("failed to start node: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
		defer cancel()
		if err := node.Stop(ctx); err != nil {
			log.Printf("Failed to stop node: %v\n", err)
		}
	}()

	log.Printf("Metrics: %s\n", tmpnet.DefaultMetricsLink(network.UUID, time.Now()))

	log.Print("Waiting for node to indicate bootstrap complete by reporting healthy\n")

	// Avoid checking too often to prevent log spam
	healthCheckInterval := 1 * time.Minute

	ctx, cancel = context.WithTimeout(context.Background(), maxDuration)
	defer cancel()
	if err := tmpnet.WaitForHealthyWithInterval(ctx, node, healthCheckInterval); err != nil {
		return fmt.Errorf("node failed to become healthy before timeout: %w", err)
	}

	log.Print("Bootstrap completed successfully!\n")

	return nil
}
