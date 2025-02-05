// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package faultinjection

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ = ginkgo.Describe("Logging auto amplification", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should ensure that a node that fails a health check amplifies its logs to debug level", func() {
		network := tmpnet.NewDefaultNetwork("avalanchego-logging-auto-amplification")
		e2e.GetEnv(tc).StartPrivateNetwork(network)

		// we'll use nodes 0 and 1, so make sure they exist to not crash with an out-of-bounds panic
		require.Len(network.Nodes, 2)
		e2e.EmitMetricsLink = false

		node1 := network.Nodes[0]

		tc.By("creating another node")
		node := e2e.AddEphemeralNode(tc, network, tmpnet.FlagsMap{
			config.LogLevelKey:                            logging.Info.String(),
			config.LogDisplayLevelKey:                     logging.Info.String(),
			config.LoggingAutoAmplificationMaxDurationKey: "7s",
			config.LogAutoAmplificationKey:                "true",
		})
		e2e.WaitForHealthy(tc, node)

		tc.By("checking that the node is connected to its peers")
		checkConnectedPeers(tc, network.Nodes, node)

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		hasDebugLogs := func(s string) bool {
			return strings.Contains(s, "DEBUG")
		}
		waitForDebugLogsToAppear := waitUntilLogSatisfiesPredicate(require, node, ctx, hasDebugLogs)

		waitUntilNodeDoesNotLogDebugForSevenSeconds := func() {
			require.NoError(waitUntilLogDoesNotSatisfyPredicate(require, node, ctx, hasDebugLogs, 7*time.Second))
		}

		tc.By("stopping the first node")
		require.NoError(node1.Stop(tc.DefaultContext()))

		tc.By("checking that the new node becomes unhealthy within timeout")
		waitUntilUnhealthy(require, node.URI)

		tc.By("checking that the new node amplifies its logging level to DEBUG")
		waitForDebugLogsToAppear()
		require.NoError(ctx.Err(), "Timed out waiting for debug logs to appear")

		tc.By("checking that the new node eventually ceases logging in debug level")
		start := time.Now()
		waitUntilNodeDoesNotLogDebugForSevenSeconds()
		require.Greater(time.Since(start), 7*time.Second)
	})
})

func waitUntilLogDoesNotSatisfyPredicate(require *require.Assertions, node *tmpnet.Node, ctx context.Context, pred func(string) bool, duration time.Duration) error {
	for {
		context, cancel := context.WithTimeout(context.Background(), duration)
		waitUntil := waitUntilLogSatisfiesPredicate(require, node, context, pred)
		waitUntil()
		cancel()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-context.Done():
			return nil
		}
	}
}

func waitUntilLogSatisfiesPredicate(require *require.Assertions, node *tmpnet.Node, ctx context.Context, pred func(string) bool) func() {
	logPath := filepath.Join(node.GetDataDir(), "logs", "main.log")

	// Open the file
	file, err := os.Open(logPath)
	require.NoError(err)

	fileInfo, err := file.Stat()
	require.NoError(err)

	_, err = file.Seek(fileInfo.Size(), 0)
	require.NoError(err)

	reader := bufio.NewReader(file)

	return func() {
		defer file.Close()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line, err := reader.ReadString('\n')
			if err == io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			require.NoError(err)

			if pred(line) {
				return
			}
		}
	}
}

func waitUntilUnhealthy(require *require.Assertions, nodeURI string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			reply, err := tmpnet.CheckNodeHealth(ctx, nodeURI)
			require.NoError(err)
			if !reply.Healthy {
				return
			}
		case <-ctx.Done():
			ginkgo.Fail("timed out waiting for new node to declare itself unhealthy")
		}
	}
}
