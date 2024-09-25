// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSubnetConversion(t *testing.T) {
	require := require.New(t)

	msg, err := NewSubnetConversion(ids.GenerateTestID())
	require.NoError(err)

	parsed, err := ParseSubnetConversion(msg.Bytes())
	require.NoError(err)
	require.Equal(msg, parsed)
}
