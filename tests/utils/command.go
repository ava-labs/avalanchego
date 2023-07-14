// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ethereum/go-ethereum/log"
	"github.com/go-cmd/cmd"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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
	ginkgo.It("ping the network", ginkgo.Label("ping"), func() {
		client := health.NewClient(DefaultLocalNodeURI)
		healthy, err := client.Readiness(context.Background(), nil)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(healthy.Healthy).Should(gomega.BeTrue())
	})
}

// At boot time subnets are created, one for each test suite. This global
// variable has all the subnets IDs that can be used.
//
// One process creates the AvalancheGo node and all the subnets, and these
// subnets IDs are passed to all other processes and stored in this global
// variable
var BlockchainIDs map[string]string

// Timeout to boot the AvalancheGo node
var bootAvalancheNodeTimeout = 5 * time.Minute

// Timeout for the health API to check the AvalancheGo is ready
var healthCheckTimeout = 5 * time.Second

func RegisterNodeRun() {
	// Keep track of the AvalancheGo external bash script, it is null for most
	// processes except the first process that starts AvalancheGo
	var startCmd *cmd.Cmd

	// Our test suite runs in a separated processes, ginkgo hasI
	// SynchronizedBeforeSuite() which is promised to run once, and its output is
	// passed over to each worker.
	//
	// In here an AvalancheGo node instance is started, and subnets are created for
	// each test case. Each test case has its own subnet, therefore all tests can
	// run in parallel without any issue.
	//
	// This function also compiles all the solidity contracts
	var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
		ctx, cancel := context.WithTimeout(context.Background(), bootAvalancheNodeTimeout)
		defer cancel()

		wd, err := os.Getwd()
		gomega.Expect(err).Should(gomega.BeNil())
		log.Info("Starting AvalancheGo node", "wd", wd)
		cmd, err := RunCommand("./scripts/run.sh")
		startCmd = cmd
		gomega.Expect(err).Should(gomega.BeNil())

		// Assumes that startCmd will launch a node with HTTP Port at [utils.DefaultLocalNodeURI]
		healthClient := health.NewClient(DefaultLocalNodeURI)
		healthy, err := health.AwaitReady(ctx, healthClient, healthCheckTimeout, nil)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(healthy).Should(gomega.BeTrue())
		log.Info("AvalancheGo node is healthy")

		blockchainIds := make(map[string]string)
		files, err := filepath.Glob("./tests/precompile/genesis/*.json")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		for _, file := range files {
			basename := filepath.Base(file)
			index := basename[:len(basename)-5]
			blockchainIds[index] = CreateNewSubnet(ctx, file)
		}

		blockchainIDsBytes, err := json.Marshal(blockchainIds)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return blockchainIDsBytes
	}, func(data []byte) {
		err := json.Unmarshal(data, &BlockchainIDs)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	// SynchronizedAfterSuite() takes two functions, the first runs after each test suite is done and the second
	// function is executed once when all the tests are done. This function is used
	// to gracefully shutdown the AvalancheGo node.
	var _ = ginkgo.SynchronizedAfterSuite(func() {}, func() {
		gomega.Expect(startCmd).ShouldNot(gomega.BeNil())
		gomega.Expect(startCmd.Stop()).Should(gomega.BeNil())
	})
}
