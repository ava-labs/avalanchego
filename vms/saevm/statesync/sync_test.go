// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/stretchr/testify/require"
)

func runStateSync(ctx context.Context, t *testing.T, source *SummaryHandler, client *networkedSH) *Summary {
	t.Helper()

	summary, err := source.GetLastStateSummary(ctx)
	require.NoError(t, err, "GetLastStateSummary()")

	parsed, err := client.ParseStateSummary(ctx, summary.Bytes())
	require.NoError(t, err, "ParseStateSummary()")

	mode, err := client.AcceptSummary(ctx, parsed)
	require.NoErrorf(t, err, "%T.AcceptSummary()", client.SummaryHandler)
	require.Equal(t, block.StateSyncStatic, mode, "AcceptSummary() mode")

	msg, err := client.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", client.SummaryHandler)
	require.Equal(t, common.StateSyncDone, msg, "WaitForEvent() message")

	require.NoErrorf(t, client.Error(), "%T.Error()", client.SummaryHandler)

	return parsed
}

// TestStateSyncEndToEnd syncs a fresh node from a source with non-trivial
// code and storage, verifies everything reached the synced disk, then starts
// a VM over the synced database and keeps accepting blocks to prove that
// settlement of the synced state was persisted correctly.
func TestStateSyncEndToEnd(t *testing.T) {
	t.Parallel()

	const numBlocks = defaultCommitInterval + 2
	sourceVM := newVM(t)
	sourceVM.acceptBlocks(t, numBlocks)

	// Handler to state sync
	xdb := saetest.NewExecutionResultsDB()
	db := memdb.New()
	client := newNetworkedSH(t, withDatabase(db), withXDB(xdb))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	ctx := t.Context()
	summary := runStateSync(ctx, t, sourceVM.summaryHandler, client)
	require.Equal(t, uint64(defaultCommitInterval), summary.Height(), "summary at last commit boundary")

	// During the sync, the network continued processing
	sourceVM.acceptBlocks(t, numBlocks)

	// State syncer closed height index when done, but the test double can't
	// just be "re-opened" - but it can be copied.
	clonable, ok := xdb.HeightIndex.(saetest.ClonableHeightIndex)
	require.True(t, ok, "xdb.HeightIndex is not ClonableHeightIndex")
	xdb.HeightIndex = clonable.Clone()

	// catch up a new VM
	clientVM := newVM(t, withDatabase(db), withXDB(xdb), withTime(sourceVM.clock.Now()))
	lastHeight := sourceVM.lastAcceptedBlock(t).Height()
	for height := summary.Height() + 1; height <= lastHeight; height++ {
		b := sourceVM.blockAtHeight(t, height)
		parsed, err := clientVM.ParseBlock(ctx, b.Bytes())
		require.NoErrorf(t, err, "ParseBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.VerifyBlock(ctx, nil, parsed), "VerifyBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.AcceptBlock(ctx, parsed), "AcceptBlock(%d)", b.Height())
		require.NoErrorf(t, parsed.WaitUntilExecuted(ctx), "WaitUntilExecuted(%d)", b.Height())
	}

	sourceHead, err := sourceVM.LastAccepted(ctx)
	require.NoError(t, err, "source LastAccepted()")
	clientHead, err := clientVM.LastAccepted(ctx)
	require.NoError(t, err, "client LastAccepted()")
	require.Equal(t, sourceHead, clientHead, "client VM caught up to the source head")
}
