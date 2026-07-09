// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	enginecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saestatesync "github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

// TestShutdownCancelsMidSync checks that shutting down while a sync is in
// flight cancels it: Shutdown returns without error, WaitForEvent reports
// [enginecommon.StateSyncDone], and the cancellation is surfaced by
// [SummaryHandler.Error].
func TestShutdownCancelsMidSync(t *testing.T) {
	sut := newSUT(t, 1, 0) // genesis only, so AcceptSummary starts a sync

	// No peers are connected, so the inner sync stalls until canceled.
	s := &summary{
		summary: *saestatesync.NewSummary(common.Hash{0xde, 0xad}, commitInterval),
	}
	mode, err := sut.AcceptSummary(t.Context(), s)
	require.NoErrorf(t, err, "%T.AcceptSummary()", sut.SummaryHandler)
	require.Equalf(t, block.StateSyncStatic, mode, "%T.AcceptSummary()", sut.SummaryHandler)

	require.NoErrorf(t, sut.Shutdown(t.Context()), "%T.Shutdown()", sut.SummaryHandler)

	msg, err := sut.WaitForEvent(t.Context())
	require.NoErrorf(t, err, "%T.WaitForEvent()", sut.SummaryHandler)
	require.Equal(t, enginecommon.StateSyncDone, msg, "WaitForEvent()")
	require.ErrorIsf(t, sut.Error(t.Context()), context.Canceled, "%T.Error()", sut.SummaryHandler)
}
