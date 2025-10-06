// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/libevm/log"
	"github.com/go-cmd/cmd"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
)

// RunCommand starts the command [bin] with the given [args] and returns the command to the caller
// TODO cmd package mentions we can do this more efficiently with cmd.NewCmdOptions rather than looping
// and calling Status().
func RunCommand(bin string, args ...string) (*cmd.Cmd, error) {
	log.Info("Executing", "cmd", fmt.Sprintf("%s %s", bin, strings.Join(args, " ")))

	curCmd := cmd.NewCmd(bin, args...)
	_ = curCmd.Start()

	// to stream outputs
	ticker := time.NewTicker(10 * time.Millisecond)
	go func() {
		prevLine := ""
		for range ticker.C {
			status := curCmd.Status()
			n := len(status.Stdout)
			if n == 0 {
				continue
			}

			line := status.Stdout[n-1]
			if prevLine != line && line != "" {
				fmt.Println("[streaming output]", line)
			}

			prevLine = line
		}
	}()

	return curCmd, nil
}

func RegisterPingTest() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("ping the network", ginkgo.Label("ping"), func() {
		client := health.NewClient(DefaultLocalNodeURI)
		healthy, err := client.Readiness(context.Background(), nil)
		require.NoError(err)
		require.True(healthy.Healthy)
	})
}

// RegisterNodeRun registers a before suite that starts an AvalancheGo process to use for the e2e tests
// and an after suite that stops the AvalancheGo process
func RegisterNodeRun() {
	require := require.New(ginkgo.GinkgoT())

	// BeforeSuite starts an AvalancheGo process to use for the e2e tests
	var startCmd *cmd.Cmd
	_ = ginkgo.BeforeSuite(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		wd, err := os.Getwd()
		require.NoError(err)
		log.Info("Starting AvalancheGo node", "wd", wd)
		cmd, err := RunCommand("./scripts/run.sh")
		startCmd = cmd
		require.NoError(err)

		// Assumes that startCmd will launch a node with HTTP Port at [utils.DefaultLocalNodeURI]
		healthClient := health.NewClient(DefaultLocalNodeURI)
		healthy, err := health.AwaitReady(ctx, healthClient, HealthCheckTimeout, nil)
		require.NoError(err)
		require.True(healthy)
		log.Info("AvalancheGo node is healthy")
	})

	ginkgo.AfterSuite(func() {
		require.NotNil(startCmd)
		require.NoError(startCmd.Stop())
		// TODO add a new node to bootstrap off of the existing node and ensure it can bootstrap all subnets
		// created during the test
	})
}

// RunHardhatTests runs the hardhat tests in the given [testPath] on the blockchain with [blockchainID]
// [execPath] is the path where the test command is executed
func RunHardhatTests(ctx context.Context, blockchainID string, execPath string, testPath string) {
	chainURI := GetDefaultChainURI(blockchainID)
	RunHardhatTestsCustomURI(ctx, chainURI, execPath, testPath)
}

func RunHardhatTestsCustomURI(ctx context.Context, chainURI string, execPath string, testPath string) {
	require := require.New(ginkgo.GinkgoT())

	log.Info(
		"Executing HardHat tests on blockchain",
		"testPath", testPath,
		"ChainURI", chainURI,
	)

	// first run clean cache
	cmd := exec.CommandContext(ctx, "npx", "hardhat", "test", testPath, "--network", "local", "--no-compile")
	cmd.Dir = execPath

	log.Info("Sleeping to wait for test ping", "rpcURI", chainURI)
	require.NoError(os.Setenv("RPC_URI", chainURI))
	log.Info("Running test command", "cmd", cmd.String())

	out, err := cmd.CombinedOutput()
	fmt.Printf("\nCombined output:\n\n%s\n", string(out))
	require.NoError(err)
}
