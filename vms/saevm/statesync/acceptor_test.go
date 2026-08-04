// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// TestStateSyncEnabled checks that various configs and states will correctly
// initiate state sync or skip it, and WaitForEvent matches this behavior.
func TestStateSyncEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "disabled",
			enabled: false,
		},
		{
			name:    "enabled",
			enabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sut := newSUT(t, withEnabled(tt.enabled))

			gotEnabled, err := sut.StateSyncEnabled(t.Context())
			require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.SummaryHandler)
			assert.Equalf(t, tt.enabled, gotEnabled, "%T.StateSyncEnabled()", sut.SummaryHandler)
		})
	}
}

func TestAcceptSummary(t *testing.T) {
	const numBlocks = defaultCommitInterval

	tests := []struct {
		name          string
		summaryHeight uint64
		opts          []sutOption
		want          block.StateSyncMode
	}{
		{
			name:          "genesis_summary_skipped",
			summaryHeight: 0,
			want:          block.StateSyncSkipped,
		},
		{
			name:          "non-genesis_summary_starts_sync",
			summaryHeight: numBlocks,
			opts:          []sutOption{withoutInitialization()},
			want:          block.StateSyncStatic,
		},
		{
			name:          "sync_skipped_if_blocks_accepted",
			summaryHeight: numBlocks,
			opts:          []sutOption{withNumBlocks(1)},
			want:          block.StateSyncSkipped,
		},
		{
			name:          "sync_started_only_genesis_accepted",
			summaryHeight: numBlocks,
			want:          block.StateSyncStatic,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := newSUT(t, withNumBlocks(numBlocks))
			s, err := src.GetStateSummary(t.Context(), tt.summaryHeight)
			require.NoErrorf(t, err, "%T.GetStateSummary(%d)", src.SummaryHandler, tt.summaryHeight)

			sut := newSUT(t, append(tt.opts, withEnabled(true))...)
			mode, err := sut.AcceptSummary(t.Context(), s)
			require.NoErrorf(t, err, "%T.AcceptSummary()", sut.SummaryHandler)
			require.Equalf(t, tt.want, mode, "%T.AcceptSummary()", sut.SummaryHandler)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var (
				wantMsg = common.StateSyncDone
				wantErr error
			)
			if tt.want == block.StateSyncSkipped {
				cancel()
				wantMsg = 0
				wantErr = context.Canceled
			}
			msg, err := sut.WaitForEvent(ctx)
			assert.ErrorIsf(t, err, wantErr, "%T.WaitForEvent()", sut.SummaryHandler) //nolint:testifylint // msg is informative
			assert.Equalf(t, wantMsg, msg, "%T.WaitForEvent()", sut.SummaryHandler)
		})
	}
}
