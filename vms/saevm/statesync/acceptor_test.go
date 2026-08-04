// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// TestStateSyncEnabled checks that various configs and states will correctly
// initiate state sync or skip it, and WaitForEvent matches this behavior.
func TestStateSyncEnabled(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		wantEnabled bool
	}{
		{
			name:        "disabled is skipped",
			enabled:     false,
			wantEnabled: false,
		},
		{
			name:        "enabled starts sync",
			enabled:     true,
			wantEnabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sut := newSUT(t,
				withEnabled(tt.enabled),
				withNumBlocks(1), // initialization doesn't change result
			)

			gotEnabled, err := sut.StateSyncEnabled(t.Context())
			require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.SummaryHandler)
			assert.Equalf(t, tt.wantEnabled, gotEnabled, "%T.StateSyncEnabled()", sut.SummaryHandler)
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
			name:          "genesis summary is skipped",
			summaryHeight: 0,
			want:          block.StateSyncSkipped,
		},
		{
			name:          "non-genesis summary starts sync",
			summaryHeight: numBlocks,
			opts:          []sutOption{withoutInitialization()},
			want:          block.StateSyncStatic,
		},
		{
			name:          "sync skipped if multiple blocks accepted",
			summaryHeight: numBlocks,
			opts:          []sutOption{withNumBlocks(1)},
			want:          block.StateSyncSkipped,
		},
		{
			name:          "sync started if only genesis is accepted",
			summaryHeight: numBlocks,
			want:          block.StateSyncStatic,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The fully initialized VM spawns goroutines that outlive Shutdown()
			// so it must be constructed outside the synctest bubble.
			src := newSUT(t, withNumBlocks(numBlocks))
			s, err := src.GetStateSummary(t.Context(), tt.summaryHeight)
			require.NoErrorf(t, err, "%T.GetStateSummary(%d)", src.SummaryHandler, tt.summaryHeight)

			synctest.Test(t, func(t *testing.T) {
				// MUST be constructed inside the bubble to ensure durable blocking.
				sut := newSUT(t, append(tt.opts, withEnabled(true))...)
				mode, err := sut.AcceptSummary(t.Context(), s)
				require.NoErrorf(t, err, "%T.AcceptSummary()", sut.SummaryHandler)
				require.Equalf(t, tt.want, mode, "%T.AcceptSummary()", sut.SummaryHandler)

				// The fake clock only advances once all goroutines in the
				// bubble are durably blocked.
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()

				var (
					wantMsg = common.StateSyncDone
					wantErr error
				)
				if tt.want == block.StateSyncSkipped {
					wantMsg = 0
					wantErr = context.DeadlineExceeded
				}
				msg, err := sut.WaitForEvent(ctx)
				assert.ErrorIsf(t, err, wantErr, "%T.WaitForEvent()", sut.SummaryHandler) //nolint:testifylint // msg is informative
				assert.Equalf(t, wantMsg, msg, "%T.WaitForEvent()", sut.SummaryHandler)
			})
		})
	}
}
