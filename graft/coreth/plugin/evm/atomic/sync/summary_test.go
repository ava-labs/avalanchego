// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func TestMarshalSummary(t *testing.T) {
	atomicSummary, err := NewSummary(common.Hash{1}, 2, common.Hash{3}, common.Hash{4})
	require.NoError(t, err, "failed to create summary")

	require.Equal(t, common.Hash{1}, atomicSummary.GetBlockHash())
	require.Equal(t, uint64(2), atomicSummary.Height())
	require.Equal(t, common.Hash{3}, atomicSummary.GetBlockRoot())

	expectedBase64Bytes := "AAAAAAAAAAAAAgEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	require.Equal(t, expectedBase64Bytes, base64.StdEncoding.EncodeToString(atomicSummary.Bytes()))
	expectedID := ids.FromStringOrPanic("256pj4a3SBG5kervhxKfeKpNRcVR1xk5BzTpkTkybkM8uMPu6Q")
	require.Equal(t, expectedID, atomicSummary.ID())

	parser := NewSummaryParser()
	called := false
	acceptImplTest := func(message.Syncable) (block.StateSyncMode, error) {
		called = true
		return block.StateSyncSkipped, nil
	}
	s, err := parser.Parse(atomicSummary.Bytes(), acceptImplTest)
	require.NoError(t, err, "failed to parse summary")
	require.Equal(t, atomicSummary.GetBlockHash(), s.GetBlockHash())
	require.Equal(t, atomicSummary.Height(), s.Height())
	require.Equal(t, atomicSummary.GetBlockRoot(), s.GetBlockRoot())
	require.Equal(t, atomicSummary.AtomicRoot, s.(*Summary).AtomicRoot)
	require.Equal(t, atomicSummary.Bytes(), s.Bytes())

	mode, err := s.Accept(t.Context())
	require.NoError(t, err, "failed to accept summary")
	require.Equal(t, block.StateSyncSkipped, mode)
	require.True(t, called)
}
