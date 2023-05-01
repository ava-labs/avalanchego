// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHash(t *testing.T) {
	require := require.New(t)

	h := Hash{}
	require.NoError(h.Set("heytherepal"))
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
