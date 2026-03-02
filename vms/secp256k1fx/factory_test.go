// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFactory(t *testing.T) {
	require := require.New(t)
	factory := Factory{}
	require.Equal(&Fx{}, factory.New())
}
