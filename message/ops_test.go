// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNumOps guards the invariant that [NumOps] is the last entry of the [Op]
// const block: every op below it must have a String and it must not.
func TestNumOps(t *testing.T) {
	require := require.New(t)

	seen := make(map[string]Op, NumOps)
	for op := range NumOps {
		s := op.String()
		require.NotEqualf("unknown", s, "op %d has no String; NumOps must remain the last entry of the Op const block", op)
		prev, ok := seen[s]
		require.Falsef(ok, "ops %d and %d share the String %q", prev, op, s)
		seen[s] = op
	}
	require.Equal("unknown", NumOps.String())
}
