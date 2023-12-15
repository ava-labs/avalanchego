// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeSerialization(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	node := NewNode(tmpDir)
	require.NoError(node.EnsureKeys())
	require.NoError(node.WriteConfig())

	loadedNode, err := ReadNode(tmpDir)
	require.NoError(err)
	require.Equal(node, loadedNode)
}
