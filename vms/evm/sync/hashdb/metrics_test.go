// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// TestHandlerMetrics pins how the responder counts served, missing-root, and
// malformed requests, and that a served request observes its durations and
// sizes.
func TestHandlerMetrics(t *testing.T) {
	t.Parallel()

	const numLeaves = 10
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, numLeaves)

	m := newTestHandlerMetrics(t)
	r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength, m)
	nodeID := ids.GenerateTestNodeID()
	ctx := t.Context()

	resp, appErr := r.Respond(ctx, nodeID, &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: maxLimit,
	})
	require.Nil(t, appErr, "Respond() on a servable request")
	require.Len(t, resp.GetKeys(), numLeaves, "Respond() leaves")
	require.Equal(t, 1.0, testutil.ToFloat64(m.count), "leafs_request_count")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.processingTime), "leafs_request_processing_time observations")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.readTime), "leafs_request_read_time observations")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.totalLeafs), "leafs_request_total_leafs observations")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.proofValsReturned), "leafs_request_proof_vals_returned observations")

	// A root this node never held counts missing_root, not invalid.
	_, appErr = r.Respond(ctx, nodeID, &syncpb.GetLeafRequest{
		RootHash: common.Hash{0x01}.Bytes(),
		KeyLimit: maxLimit,
	})
	require.ErrorIs(t, appErr, errRootNotFound, "Respond() on an unknown root")
	require.Equal(t, 1.0, testutil.ToFloat64(m.missingRoot), "leafs_request_missing_root")
	require.Zero(t, testutil.ToFloat64(m.invalid), "leafs_request_invalid after a missing root")

	// A malformed request counts invalid.
	_, appErr = r.Respond(ctx, nodeID, &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: 0,
	})
	require.ErrorIs(t, appErr, errZeroKeyLimit, "Respond() on a malformed request")
	require.Equal(t, 1.0, testutil.ToFloat64(m.invalid), "leafs_request_invalid")

	require.Equal(t, 3.0, testutil.ToFloat64(m.count), "leafs_request_count includes rejected requests")
}

// TestHandlerMetricsSnapshot pins that a snapshot-served request counts the
// fast-path attempt and its one-shot success.
func TestHandlerMetricsSnapshot(t *testing.T) {
	t.Parallel()

	const numLeaves = 10
	trieDB := synctest.NewTrieDB()
	root, _, _, snap := synctest.FillAccountTrie(t, trieDB, numLeaves)

	m := newTestHandlerMetrics(t)
	r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength, m, WithSnapshot(snap))

	resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: maxLimit,
	})
	require.Nil(t, appErr, "Respond() with a snapshot")
	require.Len(t, resp.GetKeys(), numLeaves, "Respond() leaves")

	require.Equal(t, 1.0, testutil.ToFloat64(m.snapshotReadAttempt), "leafs_request_snapshot_read_attempt")
	require.Equal(t, 1.0, testutil.ToFloat64(m.snapshotReadSuccess), "leafs_request_snapshot_read_success")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.snapshotReadTime), "leafs_request_snapshot_read_time observations")
	require.Zero(t, testutil.ToFloat64(m.snapshotSegmentInvalid), "leafs_request_snapshot_segment_invalid on an agreeing snapshot")
}
