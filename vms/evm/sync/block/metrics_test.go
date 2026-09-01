// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// TestHandlerMetrics pins how the responder counts served requests and
// requests for blocks it does not hold, and that a served request observes
// its duration and block count.
func TestHandlerMetrics(t *testing.T) {
	t.Parallel()

	chain := synctest.MakeChain(t, 5)
	db := synctest.NewBlockDB(chain)
	tip := chain[len(chain)-1]

	m := newTestHandlerMetrics(t)
	r := &responder{
		log:     loggingtest.New(t, logging.Debug),
		db:      db,
		metrics: m,
	}
	nodeID := ids.GenerateTestNodeID()
	ctx := t.Context()

	resp, appErr := r.Respond(ctx, nodeID, &syncpb.GetBlockRequest{
		Height:     tip.NumberU64(),
		NumParents: 2,
	})
	require.Nil(t, appErr, "Respond() on a servable request")
	require.Len(t, resp.GetBlocks(), 3, "Respond() blocks")
	require.Equal(t, 1.0, testutil.ToFloat64(m.count), "block_request_count")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.processingTime), "block_request_processing_time observations")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.totalBlocks), "block_request_total_blocks observations")

	_, appErr = r.Respond(ctx, nodeID, &syncpb.GetBlockRequest{
		Height: tip.NumberU64() + 100,
	})
	require.ErrorIs(t, appErr, errBlockNotFound, "Respond() on an unknown block")
	require.Equal(t, 1.0, testutil.ToFloat64(m.missingBlockHash), "block_request_missing_block_hash")

	require.Equal(t, 2.0, testutil.ToFloat64(m.count), "block_request_count includes rejected requests")
}
