// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFactory(t *testing.T) {
	require := require.New(t)
	factory := Factory{}
	fx, err := factory.New(nil)
	require.NoError(err)
	require.NotNil(fx)
}
