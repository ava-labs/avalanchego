// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	require.True(h.Check("heytherepal"))
	require.False(h.Check("heytherepal!"))
	require.False(h.Check(""))
}
