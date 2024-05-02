// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// Initialize the db volumes for a docker-compose configuration. The returned network will be updated with
// subnet and chain IDs and the target path will be configured with volumes for each node containing the
// initialized db state.
func InitDBVolumes(network *tmpnet.Network, avalancheGoPath string, pluginDir string, targetPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()
	if err := tmpnet.StartNewNetwork(
		ctx,
		os.Stdout,
		network,
		"",
		avalancheGoPath,
		pluginDir,
		0, // nodeCount is 0 because nodes should already be configured for subnets
	); err != nil {
		return fmt.Errorf("failed to start network: %w", err)
	}
	// Since the goal is to initialize the DB, we can stop the network after it has been started successfully
	if err := network.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop network: %w", err)
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to convert target path to absolute path: %w", err)
	}

	// Create volumes in the target path and copy the db state from the nodes
	for i, node := range network.Nodes {
		sourcePath := filepath.Join(node.GetDataDir(), "db")
		destPath := filepath.Join(absPath, "volumes", getServiceName(i))
		if err := os.MkdirAll(destPath, perms.ReadWriteExecute); err != nil {
			return fmt.Errorf("failed to create volume path %q: %w", destPath, err)
		}
		cmd := exec.Command("cp", "-r", sourcePath, destPath)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to copy db state from %q to %q: %w", sourcePath, destPath, err)
		}
	}

	return nil
}
