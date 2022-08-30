// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInputVerifyNil(t *testing.T) {
	require := require.New(t)
	in := (*Input)(nil)
	require.ErrorIs(in.Verify(), errNilInput)
}
