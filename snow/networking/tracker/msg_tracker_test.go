// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestMessageTracker(t *testing.T) {
	msgTracker := NewMessageTracker()

	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2})
	noMessagesVdr := ids.NewShortID([20]byte{3})

	expectedVdr1Count := uint32(5)
	expectedVdr2Count := uint32(2)

	for i := uint32(0); i < expectedVdr1Count; i++ {
		msgTracker.Add(vdr1)
	}

	for i := uint32(0); i < expectedVdr2Count; i++ {
		msgTracker.Add(vdr2)
	}

	vdr1Count := msgTracker.OutstandingCount(vdr1)
	vdr2Count := msgTracker.OutstandingCount(vdr2)
	noMessagesCount := msgTracker.OutstandingCount(noMessagesVdr)

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

	vdr1Count = msgTracker.OutstandingCount(vdr1)
	vdr2Count = msgTracker.OutstandingCount(vdr2)
	noMessagesCount = msgTracker.OutstandingCount(noMessagesVdr)

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
