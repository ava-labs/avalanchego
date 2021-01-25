// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMessageTrackerNoPool(t *testing.T) {
	msgTracker := NewMessageTracker()

	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}
	noMessagesVdr := ids.ShortID{3}

	expectedVdr1Count := uint32(5)
	expectedVdr2Count := uint32(2)

	for i := uint32(0); i < expectedVdr1Count; i++ {
		msgTracker.Add(vdr1)
	}

	for i := uint32(0); i < expectedVdr2Count; i++ {
		msgTracker.Add(vdr2)
	}

	vdr1Count, _ := msgTracker.OutstandingCount(vdr1)
	vdr2Count, _ := msgTracker.OutstandingCount(vdr2)
	noMessagesCount, _ := msgTracker.OutstandingCount(noMessagesVdr)

	if vdr1Count != expectedVdr1Count {
		t.Fatalf("Found unexpected count for validator1: %d, expected: %d", vdr1Count, expectedVdr1Count)
	}

	if vdr2Count != expectedVdr2Count {
		t.Fatalf("Found unexpected count for validator2: %d, expected: %d", vdr2Count, expectedVdr2Count)
	}

	if noMessagesCount != 0 {
		t.Fatalf("Found unexpected count for validator with no messages: %d, expected: %d", noMessagesCount, 0)
	}

	for i := uint32(0); i < expectedVdr1Count; i++ {
		msgTracker.Remove(vdr1)
	}

	for i := uint32(0); i < expectedVdr2Count; i++ {
		msgTracker.Remove(vdr2)
	}

	vdr1Count, _ = msgTracker.OutstandingCount(vdr1)
	vdr2Count, _ = msgTracker.OutstandingCount(vdr2)
	noMessagesCount, _ = msgTracker.OutstandingCount(noMessagesVdr)

	if vdr1Count != 0 {
		t.Fatalf("Found unexpected count for validator1: %d, expected: %d", vdr1Count, 0)
	}

	if vdr2Count != 0 {
		t.Fatalf("Found unexpected count for validator2: %d, expected: %d", vdr2Count, 0)
	}

	if noMessagesCount != 0 {
		t.Fatalf("Found unexpected count for validator with no messages: %d, expected: %d", noMessagesCount, 0)
	}
}

func TestMessageTrackerWithPool(t *testing.T) {
	msgTracker := NewMessageTracker()

	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}

	msgTracker.AddPool(vdr1)
	msgTracker.Add(vdr1)
	msgTracker.AddPool(vdr2)

	poolCount := msgTracker.PoolCount()
	if poolCount != 2 {
		t.Fatalf("Expected pool count to be 2, but found: %d", poolCount)
	}

	total1, pool1 := msgTracker.OutstandingCount(vdr1)
	if total1 != 2 || pool1 != 1 {
		t.Fatalf("Expected (2, 1), but found (%d, %d)", total1, pool1)
	}

	total2, pool2 := msgTracker.OutstandingCount(vdr2)
	if total2 != 1 || pool2 != 1 {
		t.Fatalf("Expected (1, 1), but found (%d, %d)", total2, pool2)
	}

	msgTracker.Remove(vdr1)
	msgTracker.Remove(vdr1)
	total1, pool1 = msgTracker.OutstandingCount(vdr1)
	if total1 != 0 || pool1 != 0 {
		t.Fatalf("Expected total count and pool count to be 0, but found (%d, %d)", total1, pool1)
	}

	poolCount = msgTracker.PoolCount()
	if poolCount != 1 {
		t.Fatalf("Expected pool count to be 1, but found: %d", poolCount)
	}

	msgTracker.Remove(vdr2)
	poolCount = msgTracker.PoolCount()
	if poolCount != 0 {
		t.Fatalf("Expected pool count to be 0, but found: %d", poolCount)
	}
}
