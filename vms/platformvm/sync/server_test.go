// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// Compares the equality of two summaries, other than their acceptFunc,
// which can't be compared.
func assertSummaryEquals(require *require.Assertions, s1, s2 Summary) {
	require.Equal(s1.Bytes(), s2.Bytes())
	require.Equal(s1.Height(), s2.Height())
	require.Equal(s1.ID(), s2.ID())
	require.Equal(s1.String(), s2.String())
	require.Equal(s1.BlockRootID, s2.BlockRootID)
}

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
	summary1, err := NewSummary(
		ids.GenerateTestID(),
		0,
		ids.GenerateTestID(),
		noopOnAcceptFunc,
	)
	require.NoError(err)

	s.RecordSummary(summary1)

	// Assert server has one summary
	require.Equal(1, s.summaries.Len())

	// Assert server returns the correct summary
	gotSummary, err := s.GetLastStateSummary(context.Background())
	require.NoError(err)
	assertSummaryEquals(require, summary1, gotSummary.(Summary))

	gotSummary, err = s.GetStateSummary(context.Background(), 0)
	require.NoError(err)
	assertSummaryEquals(require, summary1, gotSummary.(Summary))

	// Still don't have summary at height 2
	_, err = s.GetStateSummary(context.Background(), 2)
	require.ErrorIs(err, database.ErrNotFound)

	// Record a second summary
	summary2, err := NewSummary(
		ids.GenerateTestID(),
		2, // Note non-consecutive height
		ids.GenerateTestID(),
		noopOnAcceptFunc,
	)
	require.NoError(err)

	s.RecordSummary(summary2)

	// Assert server has two summaries
	require.Equal(2, s.summaries.Len())

	// Assert server returns the correct summary
	gotSummary, err = s.GetLastStateSummary(context.Background())
	require.NoError(err)
	assertSummaryEquals(require, summary2, gotSummary.(Summary))

	gotSummary, err = s.GetStateSummary(context.Background(), 2)
	require.NoError(err)
	assertSummaryEquals(require, summary2, gotSummary.(Summary))

	// Still have previous summary
	gotSummary, err = s.GetStateSummary(context.Background(), 0)
	require.NoError(err)
	assertSummaryEquals(require, summary1, gotSummary.(Summary))

	calledOnAcceptFunc := 0
	onAcceptFunc := func(Summary) (block.StateSyncMode, error) {
		calledOnAcceptFunc++
		return block.StateSyncStatic, nil
	}
	// Record a third summary
	summary3, err := NewSummary(
		ids.GenerateTestID(),
		4,
		ids.GenerateTestID(),
		onAcceptFunc,
	)
	require.NoError(err)

	s.RecordSummary(summary3)

	// Assert server has two summaries
	require.Equal(2, s.summaries.Len())

	// Assert server returns the correct summary
	gotSummary, err = s.GetLastStateSummary(context.Background())
	require.NoError(err)
	assertSummaryEquals(require, summary3, gotSummary.(Summary))

	// Assert onAcceptFunc is called on summaries fetched from [s].
	mode, err := gotSummary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, mode)
	require.Equal(1, calledOnAcceptFunc)

	gotSummary, err = s.GetStateSummary(context.Background(), 4)
	require.NoError(err)
	assertSummaryEquals(require, summary3, gotSummary.(Summary))

	mode, err = gotSummary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, mode)
	require.Equal(2, calledOnAcceptFunc)

	// Still have previous summary
	gotSummary, err = s.GetStateSummary(context.Background(), 2)
	require.NoError(err)
	assertSummaryEquals(require, summary2, gotSummary.(Summary))

	// No longer have first summary
	_, err = s.GetStateSummary(context.Background(), 0)
	require.ErrorIs(err, database.ErrNotFound)
}
