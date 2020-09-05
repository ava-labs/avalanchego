// // (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// // See the file LICENSE for licensing terms.

package router

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/tracker"

	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
)

func TestTakeMessage(t *testing.T) {
	bufferSize := 8
	vdrList := make([]validators.Validator, 0, bufferSize)
	for i := 0; i < bufferSize; i++ {
		vdr := validators.GenerateRandomValidator(2)
		vdrList = append(vdrList, vdr)
	}
	nonStakerID := ids.NewShortID([20]byte{16})

	cpuTracker := tracker.NewCPUTracker(time.Second)
	msgTracker := tracker.NewMessageTracker()
	vdrs := validators.NewSet()
	vdrs.Set(vdrList)
	resourceManager := NewResourceManager(
		vdrs,
		logging.NoLog{},
		msgTracker,
		cpuTracker,
		uint32(bufferSize),
		1,   // Allow each peer to take at most one message from pool
		0.5, // Allot half of message queue to stakers
		0.5, // Allot half of CPU time to stakers
	)

	for i, vdr := range vdrList {
		if success := resourceManager.TakeMessage(vdr.ID()); !success {
			t.Fatalf("Failed to take message %d.", i)
		}
	}

	if success := resourceManager.TakeMessage(nonStakerID); success {
		t.Fatal("Should have throttled message from non-staker when the message pool was empty")
	}

	for _, vdr := range vdrList {
		resourceManager.ReturnMessage(vdr.ID())
	}

	if success := resourceManager.TakeMessage(nonStakerID); !success {
		t.Fatal("Failed to take additional message after all previous messages were marked as done.")
	}
}

func TestStakerGetsThrottled(t *testing.T) {
	bufferSize := 8
	vdrList := make([]validators.Validator, 0, bufferSize)
	for i := 0; i < bufferSize; i++ {
		vdr := validators.GenerateRandomValidator(2)
		vdrList = append(vdrList, vdr)
	}

	cpuTracker := tracker.NewCPUTracker(time.Second)
	msgTracker := tracker.NewMessageTracker()
	vdrs := validators.NewSet()
	vdrs.Set(vdrList)
	resourceManager := NewResourceManager(
		vdrs,
		logging.NoLog{},
		msgTracker,
		cpuTracker,
		uint32(bufferSize),
		1,   // Allow each peer to take at most one message from pool
		0.5, // Allot half of message queue to stakers
		0.5, // Allot half of CPU time to stakers
	)

	// Ensure that a staker with only part of the stake
	// cannot take up the entire message queue
	vdrID := vdrList[0].ID()
	for i := 0; i < bufferSize; i++ {
		if success := resourceManager.TakeMessage(vdrID); !success {
			// The staker was throttled before taking up the whole message queue
			return
		}
	}
	t.Fatal("Staker should have been throttled before taking up the entire message queue.")
}
