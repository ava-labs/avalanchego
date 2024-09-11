// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeduplicate(t *testing.T) {
	require.Equal(
		t,
		[]int{0, 1, 2, 3},
		ToSlice(Deduplicate(FromSlice(0, 1, 2, 1, 2, 0, 3))),
	)
}
