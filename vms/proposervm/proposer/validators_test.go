// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestValidatorDataLess(t *testing.T) {
	require := require.New(t)

	var v1, v2 validatorData
	require.False(v1.Less(v2))
	require.False(v2.Less(v1))

	v1 = validatorData{
		id: ids.NodeID{1},
	}
	require.False(v1.Less(v2))
	require.True(v2.Less(v1))
}
