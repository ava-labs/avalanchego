// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSetSubnetValidatorWeight(t *testing.T) {
	require := require.New(t)

	msg, err := NewSetSubnetValidatorWeight(
		ids.GenerateTestID(),
		rand.Uint64(), //#nosec G404
		rand.Uint64(), //#nosec G404
	)
	require.NoError(err)

	parsed, err := ParseSetSubnetValidatorWeight(msg.Bytes())
	require.NoError(err)
	require.Equal(msg, parsed)
}
