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

// Bootstraps a local process-based network, creates its subnets and chains, and copies
// the resulting db state from one of the nodes to the provided path. The path will be
// created if it does not already exist.
func initBootstrapDB(network *tmpnet.Network, destPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()
	if err := tmpnet.BootstrapNewNetwork(
		ctx,
		tests.NewDefaultLogger(""),
		network,
		"",
	); err != nil {
		return fmt.Errorf("failed to bootstrap network: %w", err)
	}
	// Since the goal is to initialize the DB, we can stop the network after it has been started successfully
	if err := network.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop network: %w", err)
	}

	// Copy the db state from the bootstrap node to the compose volume path.
	sourcePath := filepath.Join(network.Nodes[0].DataDir, "db")
	if err := os.MkdirAll(destPath, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create db path %q: %w", destPath, err)
	}
	// TODO(marun) Replace with os.CopyFS once we upgrade to Go 1.23
	cmd := exec.Command("cp", "-r", sourcePath, destPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy bootstrap db from %q to %q: %w", sourcePath, destPath, err)
	}

	return nil
}
