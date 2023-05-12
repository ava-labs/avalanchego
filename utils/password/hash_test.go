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
	err := h.Set("heytherepal")
	require.NoError(err)
	require.True(h.Check("heytherepal"))
	require.False(h.Check("heytherepal!"))
	require.False(h.Check(""))
}
