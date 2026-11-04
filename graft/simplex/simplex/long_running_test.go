// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"math"
	"testing"

	"github.com/ava-labs/simplex/testutil/long_running"
)

func TestLongRunningSimple(t *testing.T) {
	net := long_running.NewDefaultLongRunningNetwork(t, 5)

	net.StartInstances()
	net.WaitForNodesToEnterRound(40)
	net.StopAndAssert(false)
}

func TestLongRunningReplication(t *testing.T) {
	net := long_running.NewDefaultLongRunningNetwork(t, 10)
	net.StartInstances()

	net.WaitForNodesToEnterRound(40)
	net.NoMoreBlocks()
	net.DisconnectNodes(2)
	net.ContinueBlocks()
	net.WaitForNodesToEnterRound(70, 1, 3, 4, 5, 6)
	net.DisconnectNodes(4)
	net.WaitForNodesToEnterRound(90, 1, 3, 5, 6, 7, 8, 9)
	net.ConnectNodes(2, 4)
	net.WaitForNodesToEnterRound(150)
	net.StopAndAssert(false)
}

func TestLongRunningCrash(t *testing.T) {
	net := long_running.NewDefaultLongRunningNetwork(t, 10)

	net.StartInstances()
	net.WaitForNodesToEnterRound(30)
	net.CrashNodes(3)
	crashedNodeLatestBlock := net.GetInstance(3).Storage.NumBlocks()

	net.WaitForNodesToEnterRound(80, 1, 2, 4, 5, 6, 7, 8, 9)
	net.StartNodes(3)

	waitForRound := math.Max(float64(crashedNodeLatestBlock*2), 150)
	net.WaitForNodesToEnterRound(uint64(waitForRound))
	net.StopAndAssert(false)
}
