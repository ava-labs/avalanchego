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
		want        block.StateSyncMode
	}{
		{
			name:        "explicitly disabled is skipped",
			enabled:     &disabled,
			initialized: false,
			want:        block.StateSyncSkipped,
		},
		{
			name:        "unset is skipped once a block was processed",
			enabled:     nil,
			initialized: true,
			want:        block.StateSyncSkipped,
		},
		{
			name:        "unset starts sync on a fresh database",
			enabled:     nil,
			initialized: false,
			want:        block.StateSyncStatic,
		},
		{
			name:        "explicitly enabled starts sync",
			enabled:     &enabled,
			initialized: false,
			want:        block.StateSyncStatic,
		},
		{
			name:        "explicitly enabled starts sync even after a block was processed",
			enabled:     &enabled,
			initialized: true,
			want:        block.StateSyncStatic,
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

			enabled, err := sut.StateSyncEnabled(t.Context())
			require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.SummaryHandler)
			assert.Equalf(t, tt.want != block.StateSyncSkipped, enabled, "%T.StateSyncEnabled()", sut.SummaryHandler)

			mode, err := sut.AcceptSummary(t.Context(), &Summary{})
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
