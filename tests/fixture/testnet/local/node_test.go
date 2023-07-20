// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeSerialization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	require.NoError(t, err)

	node := NewLocalNode(tmpDir)
	require.NoError(t, node.EnsureKeys())
	require.NoError(t, node.WriteConfig())

	loadedNode, err := ReadNode(tmpDir)
	require.NoError(t, err)
	require.Equal(t, node, loadedNode)
}
