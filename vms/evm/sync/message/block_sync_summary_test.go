// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func TestBlockSyncSummary_MarshalGolden(t *testing.T) {
	t.Parallel()

	blockSyncSummary, err := NewBlockSyncSummary(common.Hash{1}, 2, common.Hash{3})
	require.NoError(t, err)

	require.Equal(t, common.Hash{1}, blockSyncSummary.GetBlockHash())
	require.Equal(t, uint64(2), blockSyncSummary.Height())
	require.Equal(t, common.Hash{3}, blockSyncSummary.GetBlockRoot())

	expectedBase64Bytes := "AAAAAAAAAAAAAgEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	require.Equal(t, expectedBase64Bytes, base64.StdEncoding.EncodeToString(blockSyncSummary.Bytes()))
}

func TestBlockSyncSummary_Methods(t *testing.T) {
	t.Parallel()

	blockHash := common.Hash{1, 2, 3}
	blockNumber := uint64(42)
	blockRoot := common.Hash{4, 5, 6}

	summary, err := NewBlockSyncSummary(blockHash, blockNumber, blockRoot)
	require.NoError(t, err)

	require.Equal(t, blockHash, summary.GetBlockHash())
	require.Equal(t, blockRoot, summary.GetBlockRoot())
	require.Equal(t, blockNumber, summary.Height())
	require.NotNil(t, summary.Bytes())
	require.NotEqual(t, ids.ID{}, summary.ID())

	// Test String() method
	str := summary.String()
	require.Contains(t, str, "BlockSyncSummary")
	require.Contains(t, str, blockHash.String())
}

func TestBlockSyncSummary_Accept(t *testing.T) {
	t.Parallel()

	errTestError := errors.New("test error")

	tests := []struct {
		name       string
		acceptImpl AcceptImplFn
		wantErr    error
	}{
		{
			name:    "nil_acceptImpl",
			wantErr: errAcceptImplNotSpecified,
		},
		{
			name: "with_acceptImpl_error",
			acceptImpl: func(Syncable) (block.StateSyncMode, error) {
				return block.StateSyncSkipped, errTestError
			},
			wantErr: errTestError,
		},
		{
			name: "with_acceptImpl_success",
			acceptImpl: func(Syncable) (block.StateSyncMode, error) {
				return block.StateSyncSkipped, nil
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary, err := NewBlockSyncSummary(common.Hash{1}, 1, common.Hash{2})
			require.NoError(t, err)

			summary.acceptImpl = tc.acceptImpl

			mode, err := summary.Accept(t.Context())
			require.Equal(t, block.StateSyncSkipped, mode)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestBlockSyncSummaryParser_ParseValid(t *testing.T) {
	t.Parallel()

	blockSyncSummary, err := NewBlockSyncSummary(common.Hash{1}, 2, common.Hash{3})
	require.NoError(t, err)

	parser := NewBlockSyncSummaryParser()
	called := false
	acceptImplTest := func(Syncable) (block.StateSyncMode, error) {
		called = true
		return block.StateSyncSkipped, nil
	}
	s, err := parser.Parse(blockSyncSummary.Bytes(), acceptImplTest)
	require.NoError(t, err)
	require.Equal(t, blockSyncSummary.GetBlockHash(), s.GetBlockHash())
	require.Equal(t, blockSyncSummary.Height(), s.Height())
	require.Equal(t, blockSyncSummary.GetBlockRoot(), s.GetBlockRoot())
	require.Equal(t, blockSyncSummary.Bytes(), s.Bytes())

	mode, err := s.Accept(t.Context())
	require.NoError(t, err)
	require.Equal(t, block.StateSyncSkipped, mode)
	require.True(t, called)
}

func TestBlockSyncSummaryParser_ParseInvalid(t *testing.T) {
	t.Parallel()

	parser := NewBlockSyncSummaryParser()

	tests := []struct {
		name         string
		summaryBytes []byte
		wantErr      error
	}{
		{
			name:         "invalid_bytes",
			summaryBytes: []byte("not-a-summary"),
			wantErr:      errInvalidBlockSyncSummary,
		},
		{
			name:         "empty_bytes",
			summaryBytes: []byte{},
			wantErr:      errInvalidBlockSyncSummary,
		},
		{
			name:         "nil_bytes",
			summaryBytes: nil,
			wantErr:      errInvalidBlockSyncSummary,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parser.Parse(tc.summaryBytes, nil)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
