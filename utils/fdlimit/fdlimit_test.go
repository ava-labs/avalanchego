// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fdlimit

import "testing"

// Based off of Quorum implementation:
// https://github.com/ConsenSys/quorum/tree/c215989c10f191924e6b5b668ba4ed8ed425ded1/common/fdlimit

func TestFileDescriptorLimits(t *testing.T) {
	_, _, err := GetLimit()
	if err != nil {
		t.Fatalf("Failed to get file descriptor limits: %s", err)
	}

	if err := RaiseLimit(DefaultFdLimit); err != nil {
		t.Fatalf("Failed to raise file descriptor limit: %s", err)
	}

	cur, _, err := GetLimit()
	if err != nil {
		t.Fatalf("Failed to get file descriptor limits after attempting to raise the limit: %s", err)
	}
	if cur < DefaultFdLimit {
		t.Fatalf("Failed to raise the file descriptor limit. Target: %d. Current Value: %d.", DefaultFdLimit, cur)
	}
}
