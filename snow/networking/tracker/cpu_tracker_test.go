// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/uptime"
)

func TestCPUTracker(t *testing.T) {
	halflife := time.Second
	cpuTracker := NewCPUTracker(uptime.ContinuousFactory{}, halflife)
	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}

	startTime1 := time.Now()
	endTime1 := startTime1.Add(halflife)

	cpuTracker.StartCPU(vdr1, startTime1)
	cpuTracker.StopCPU(vdr1, endTime1)

	startTime2 := endTime1
	endTime2 := startTime2.Add(halflife)
	cpuTracker.StartCPU(vdr2, startTime2)
	cpuTracker.StopCPU(vdr2, endTime2)

	utilization1 := cpuTracker.Utilization(vdr1, endTime2)
	utilization2 := cpuTracker.Utilization(vdr2, endTime2)

	if utilization1 >= utilization2 {
		t.Fatalf("Utilization should have been higher for the more recent spender")
	}

	cumulative := cpuTracker.CumulativeUtilization(endTime2)
	sum := utilization1 + utilization2
	if cumulative != sum {
		t.Fatalf("Cumulative utilization: %f should have been equal to the sum of the spenders: %f", cumulative, sum)
	}

	expectedLen := 2
	len := cpuTracker.Len()
	if len != expectedLen {
		t.Fatalf("Expected length to match number of spenders: %d, but found length: %d", expectedLen, len)
	}

	// Set pruning time to 64 halflifes in the future, to guarantee that
	// any counts should have gone to 0
	pruningTime := endTime2.Add(halflife * 64)
	cpuTracker.CumulativeUtilization(pruningTime)
	len = cpuTracker.Len()
	if len != 0 {
		t.Fatalf("Expected length to be 0 after pruning, but found length: %d", len)
	}
}
