// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestServer(t *testing.T) {
	require := require.New(t)

	s := NewServer(2)

	// Assert server is empty
	require.NotNil(s)
	require.Zero(s.summaries.Len())

	_, err := s.GetLastStateSummary(context.Background())
	require.ErrorIs(err, database.ErrNotFound)

	_, err = s.GetStateSummary(context.Background(), 0)
	require.ErrorIs(err, database.ErrNotFound)

	// Record a summary
	summary1, err := NewSyncSummary(
		ids.GenerateTestID(),
		0,
		ids.GenerateTestID(),
	)
	require.NoError(err)

	s.RecordSummary(summary1)

	// Assert server has one summary
	require.Equal(1, s.summaries.Len())

	// Assert server returns the correct summary
	gotSummary, err := s.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(summary1, gotSummary)

	gotSummary, err = s.GetStateSummary(context.Background(), 0)
	require.NoError(err)
	require.Equal(summary1, gotSummary)

	// Still don't have summary at height 2
	_, err = s.GetStateSummary(context.Background(), 2)
	require.ErrorIs(err, database.ErrNotFound)

	// Record a second summary
	summary2, err := NewSyncSummary(
		ids.GenerateTestID(),
		2, // Note non-consecutive height
		ids.GenerateTestID(),
	)
	require.NoError(err)

	s.RecordSummary(summary2)

	// Assert server has two summaries
	require.Equal(2, s.summaries.Len())

	// Assert server returns the correct summary
	gotSummary, err = s.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(summary2, gotSummary)

	gotSummary, err = s.GetStateSummary(context.Background(), 2)
	require.NoError(err)
	require.Equal(summary2, gotSummary)

	// Still have previous summary
	gotSummary, err = s.GetStateSummary(context.Background(), 0)
	require.NoError(err)
	require.Equal(summary1, gotSummary)

	// Record a third summary
	summary3, err := NewSyncSummary(
		ids.GenerateTestID(),
		4,
		ids.GenerateTestID(),
	)
	require.NoError(err)

	s.RecordSummary(summary3)

	// Assert server has two summaries
	require.Equal(2, s.summaries.Len())

	// Assert server returns the correct summary
	gotSummary, err = s.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(summary3, gotSummary)

	gotSummary, err = s.GetStateSummary(context.Background(), 4)
	require.NoError(err)
	require.Equal(summary3, gotSummary)

	// Still have previous summary
	gotSummary, err = s.GetStateSummary(context.Background(), 2)
	require.NoError(err)
	require.Equal(summary2, gotSummary)

	// No longer have first summary
	_, err = s.GetStateSummary(context.Background(), 0)
	require.ErrorIs(err, database.ErrNotFound)
}
