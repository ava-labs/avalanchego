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

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// Given a path, compose the expected path of the bootstrap node's docker compose db volume.
func getBootstrapVolumePath(targetPath string) (string, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to convert target path to absolute path: %w", err)
	}
	return filepath.Join(absPath, "volumes", getServiceName(bootstrapIndex)), nil
}

// Creates the network and copies its configuration and database to the compose data volumes for use at runtime.
func initVolumes(network *tmpnet.Network, avalancheGoPath string, pluginDir string, targetPath string) error {
	err := bootstrapNetwork(network, avalancheGoPath, pluginDir)
	if err != nil {
		return nil
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to convert target path to absolute path: %w", err)
	}

	// Copy the db from one of the nodes to the bootstrap volume path.
	bootstrapVolumePath := filepath.Join(absPath, "volumes", getServiceName(bootstrapIndex))
	if err != nil {
		return fmt.Errorf("failed to get bootstrap volume path: %w", err)
	}
	if err := os.MkdirAll(bootstrapVolumePath, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create db path %q: %w", bootstrapVolumePath, err)
	}
	dbPath := filepath.Join(network.Nodes[0].GetDataDir(), "db")
	// TODO(marun) Replace with os.CopyFS once we upgrade to Go 1.23
	cmd := exec.Command("cp", "-r", dbPath, bootstrapVolumePath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy bootstrap db from %q to %q: %w", dbPath, bootstrapVolumePath, err)
	}

	// Copy the network path to the workload volume.
	workloadNetworkPath := filepath.Join(absPath, "volumes", workloadName, "network")
	if err := os.MkdirAll(workloadNetworkPath, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create network path %q: %w", workloadNetworkPath, err)
	}
	// TODO(marun) Replace with os.CopyFS once we upgrade to Go 1.23
	cmd = exec.Command("cp", "-r", network.Dir, workloadNetworkPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy network from %q to %q: %w", network.Dir, workloadNetworkPath, err)
	}

	return nil
}

// Bootstraps a local process-based network and then stops it. The resulting network dir can be
// used as a source of an initial database and configuration for an antithesis workload.
func bootstrapNetwork(network *tmpnet.Network, avalancheGoPath string, pluginDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()
	if err := tmpnet.BootstrapNewNetwork(
		ctx,
		tests.NewDefaultLogger(""),
		network,
		"",
		avalancheGoPath,
		pluginDir,
	); err != nil {
		return fmt.Errorf("failed to bootstrap network: %w", err)
	}
	// Since the goal is to copy the , we can stop the network after it has been started successfully
	if err := network.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop network: %w", err)
	}

	return nil
}
