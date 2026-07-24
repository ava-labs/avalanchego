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
	enabled, disabled := true, false
	tests := []struct {
		name        string
		enabled     *bool
		initialized bool
		wantEnabled bool
	}{
		{
			name:        "explicitly disabled is skipped",
			enabled:     &disabled,
			initialized: false,
			wantEnabled: false,
		},
		{
			name:        "unset is skipped once a block was processed",
			enabled:     nil,
			initialized: true,
			wantEnabled: false,
		},
		{
			name:        "unset starts sync on a fresh database",
			enabled:     nil,
			initialized: false,
			wantEnabled: true,
		},
		{
			name:        "explicitly enabled starts sync",
			enabled:     &enabled,
			initialized: false,
			wantEnabled: true,
		},
		{
			name:        "explicitly enabled starts sync even after a block was processed",
			enabled:     &enabled,
			initialized: true,
			wantEnabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := []sutOption{
				withEnabled(tt.enabled),
			}
			if tt.initialized {
				opts = append(opts, withNumBlocks(1))
			} else {
				opts = append(opts, withoutInitialization())
			}
			sut := newSUT(t, opts...)

			gotEnabled, err := sut.StateSyncEnabled(t.Context())
			require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.SummaryHandler)
			assert.Equalf(t, tt.wantEnabled, gotEnabled, "%T.StateSyncEnabled()", sut.SummaryHandler)
		})
	}
}

func TestAcceptSummary(t *testing.T) {
	const numBlocks = 5

	tests := []struct {
		name          string
		summaryHeight uint64
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
			want:          block.StateSyncStatic,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sut := newSUT(t, withNumBlocks(numBlocks))
			b := sut.blocks[tt.summaryHeight]
			s := NewSummary(b.Hash(), b.Height())

			mode, err := sut.AcceptSummary(t.Context(), s)
			require.NoErrorf(t, err, "%T.AcceptSummary()", sut.SummaryHandler)
			require.Equalf(t, tt.want, mode, "%T.AcceptSummary()", sut.SummaryHandler)

			type waitResult struct {
				msg common.Message
				err error
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan waitResult, 1)
			go func() {
				msg, err := sut.WaitForEvent(ctx)
				done <- waitResult{msg: msg, err: err}
			}()

			var (
				wantMsg = common.StateSyncDone
				wantErr error
			)
			if tt.want == block.StateSyncSkipped {
				cancel()
				wantMsg = 0
				wantErr = context.Canceled
			}
			r := <-done
			assert.ErrorIsf(t, r.err, wantErr, "%T.WaitForEvent()", sut.SummaryHandler) //nolint:testifylint // msg is informative
			assert.Equalf(t, wantMsg, r.msg, "%T.WaitForEvent()", sut.SummaryHandler)
		})
	}
}
