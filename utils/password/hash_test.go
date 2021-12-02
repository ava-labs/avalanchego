// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"testing"
)

func TestHash(t *testing.T) {
	h := Hash{}
	if err := h.Set("heytherepal"); err != nil {
		t.Fatal(err)
	}
	if !h.Check("heytherepal") {
		t.Fatalf("Should have verified the password")
	}
	if h.Check("heytherepal!") {
		t.Fatalf("Shouldn't have verified the password")
	}
	if h.Check("") {
		t.Fatalf("Shouldn't have verified the password")
	}
}
