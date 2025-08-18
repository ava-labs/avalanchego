// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntFormat(t *testing.T) {
	require := require.New(t)

	require.Equal("%01d", IntFormat(0))
	require.Equal("%01d", IntFormat(9))
	require.Equal("%02d", IntFormat(10))
	require.Equal("%02d", IntFormat(99))
	require.Equal("%03d", IntFormat(100))
	require.Equal("%03d", IntFormat(999))
	require.Equal("%04d", IntFormat(1000))
	require.Equal("%04d", IntFormat(9999))
	require.Equal("%05d", IntFormat(10000))
	require.Equal("%05d", IntFormat(99999))
	require.Equal("%06d", IntFormat(100000))
	require.Equal("%06d", IntFormat(999999))
}
