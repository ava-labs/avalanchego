// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmpty(t *testing.T) {
	require := require.New(t)
	noSigner := &Empty{}
	err := noSigner.Verify()
	require.NoError(err)
	require.Nil(noSigner.Key())
}
