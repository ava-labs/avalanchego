// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNone(t *testing.T) {
	require := require.New(t)

	noSigner := &None{}
	require.NoError(noSigner.Verify())
	require.Nil(noSigner.Key())
}
