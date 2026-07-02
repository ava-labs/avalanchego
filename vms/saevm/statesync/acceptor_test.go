// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
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

			db := memdb.New()
			var sut *networkedSH
			if tt.initialized {
				_ = newVM(t, withDatabase(db)) // side effect of committing genesis
			}
			sut = newNetworkedSH(t, withEnabled(tt.enabled), withDatabase(db))

			enabled, err := sut.StateSyncEnabled(t.Context())
			require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.SummaryHandler)
			assert.Equalf(t, tt.want != block.StateSyncSkipped, enabled, "%T.StateSyncEnabled()", sut.SummaryHandler)
		})
	}
}

func TestStateSyncSkipAccept(t *testing.T) {
	sut := newNetworkedSH(t, withEnabled(utils.PointerTo(false)))
	mode, err := sut.AcceptSummary(t.Context(), &Summary{})
	require.NoErrorf(t, err, "%T.AcceptSummary()", sut.SummaryHandler)
	require.Equalf(t, block.StateSyncSkipped, mode, "%T.AcceptSummary()", sut.SummaryHandler)
	require.NoErrorf(t, sut.Error(), "%T.Error()", sut.SummaryHandler)
}

func TestWaitForEventCanceled(t *testing.T) {
	sut := newNetworkedSH(t)

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

	cancel()
	r := <-done
	assert.ErrorIsf(t, r.err, context.Canceled, "%T.WaitForEvent()", sut.SummaryHandler) //nolint:testifylint // msg is informative
	assert.Equalf(t, common.Message(0), r.msg, "%T.WaitForEvent()", sut.SummaryHandler)
}
