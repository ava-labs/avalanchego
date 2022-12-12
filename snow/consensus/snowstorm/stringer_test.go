// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSnowballNodeLess(t *testing.T) {
	require := require.New(t)

	node1 := &snowballNode{
		txID: ids.ID{},
	}
	node2 := &snowballNode{
		txID: ids.ID{},
	}
	require.False(node1.Less(node2))
	require.False(node2.Less(node1))

	node1 = &snowballNode{
		txID: ids.ID{1},
	}
	node2 = &snowballNode{
		txID: ids.ID{},
	}
	require.False(node1.Less(node2))
	require.True(node2.Less(node1))

	node1 = &snowballNode{
		txID: ids.ID{1},
	}
	node2 = &snowballNode{
		txID: ids.ID{1},
	}
	require.False(node1.Less(node2))
	require.False(node2.Less(node1))

	node1 = &snowballNode{
		txID: ids.ID{1},
	}
	node2 = &snowballNode{
		txID: ids.ID{1, 2},
	}
	require.True(node1.Less(node2))
	require.False(node2.Less(node1))
}
