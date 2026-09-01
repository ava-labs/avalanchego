// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// TestHandlerMetrics pins how the responder counts served and rejected
// requests, and that a served request observes its read time and bytes.
func TestHandlerMetrics(t *testing.T) {
	t.Parallel()

	codeHash, codeBytes := randomCode(t)
	db := memorydb.New()
	writeCode(db, codes{codeHash: codeBytes})

	m := newTestHandlerMetrics(t)
	r := newResponder(loggingtest.New(t, logging.Debug), db, m)
	nodeID := ids.GenerateTestNodeID()
	ctx := t.Context()

	resp, appErr := r.Respond(ctx, nodeID, &syncpb.GetCodeRequest{Hashes: [][]byte{codeHash.Bytes()}})
	require.Nil(t, appErr, "Respond() on a servable request")
	require.Len(t, resp.GetData(), 1, "Respond() code blobs")
	require.Equal(t, 1.0, testutil.ToFloat64(m.count), "code_request_count")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.readTime), "code_request_read_time observations")
	require.Equal(t, uint64(1), synctest.HistogramSampleCount(t, m.bytesReturned), "code_request_bytes_returned observations")

	_, appErr = r.Respond(ctx, nodeID, &syncpb.GetCodeRequest{Hashes: [][]byte{{0x01}}})
	require.ErrorIs(t, appErr, errHashNotFound, "Respond() on a missing hash")
	require.Equal(t, 1.0, testutil.ToFloat64(m.missingCodeHash), "code_request_missing_code_hash")

	_, appErr = r.Respond(ctx, nodeID, &syncpb.GetCodeRequest{
		Hashes: hashBytes(make([]common.Hash, maxHashesPerRequest+1)),
	})
	require.ErrorIs(t, appErr, errTooManyHashes, "Respond() on too many hashes")
	require.Equal(t, 1.0, testutil.ToFloat64(m.tooManyHashes), "code_request_too_many_hashes")

	_, appErr = r.Respond(ctx, nodeID, &syncpb.GetCodeRequest{
		Hashes: [][]byte{codeHash.Bytes(), codeHash.Bytes()},
	})
	require.ErrorIs(t, appErr, errDuplicateHashes, "Respond() on duplicate hashes")
	require.Equal(t, 1.0, testutil.ToFloat64(m.duplicateHashes), "code_request_duplicate_hashes")

	require.Equal(t, 4.0, testutil.ToFloat64(m.count), "code_request_count includes rejected requests")
}
