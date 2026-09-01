// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	saestatesync "github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

// TestSyncCanceled checks that a stalled Sync returns once its context is
// canceled.
func TestSyncCanceled(t *testing.T) {
	sut := newSUT(t)

	s := &summary{
		summary: *saestatesync.NewSummary(common.Hash{0xde, 0xad}, commitInterval),
	}
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		// No peers are connected, so the inner sync stalls until canceled.
		errCh <- sut.Sync(ctx, s)
	}()
	cancel()
	require.ErrorIsf(t, <-errCh, context.Canceled, "%T.Sync() after cancel", sut.SummaryHandler)
}
