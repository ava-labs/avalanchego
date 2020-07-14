// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"
)

func TestTxVerify(t *testing.T) {
	Setup()

	if err := Red.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestTxBytes(t *testing.T) {
	Setup()

	if Red.Bytes() == nil {
		t.Fatalf("Expected non-nil bytes")
	}
}
