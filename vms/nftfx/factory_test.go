// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFactory(t *testing.T) {
	require := require.New(t)

	factory := Factory{}
	require.Equal(&Fx{}, factory.New())
}
