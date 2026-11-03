// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/eventlog"
)

// TestJudgePaths feeds judgePaths the verdicts of the block, API, and gossip
// paths on one tx and one state, in the order of the chain time each ran at.
func TestJudgePaths(t *testing.T) {
	const (
		// A fresh API refusal by the executor, as service.go IssueTx and
		// network/gossip.go gossipMempool.Add wrap it.
		apiMint = "couldn't issue tx: failed verification: failed execution: insufficient unlocked funds"
		// The drop reason of a gossiped tx that the executor refused, as
		// gossipMempool.Add marks it.
		gossipMint = "failed execution: insufficient unlocked funds"
	)
	took := func(path string) verdict { return verdict{Path: path} }
	refused := func(path, reason string) verdict { return verdict{Path: path, Refusal: reason} }

	tests := []struct {
		name     string
		verdicts []verdict
		wantErr  error
	}{
		{
			name:     "all_took",
			verdicts: []verdict{took(pathBlock), took(pathAPI), took(pathGossip)},
		},
		{
			name:     "all_refused",
			verdicts: []verdict{refused(pathBlock, "insufficient unlocked funds"), refused(pathAPI, apiMint), refused(pathGossip, gossipMint)},
		},
		{
			name:     "block_refused_mempool_took",
			verdicts: []verdict{refused(pathBlock, "insufficient unlocked funds"), took(pathAPI), took(pathGossip)},
		},
		{
			name:     "api_refused_gossip_took",
			verdicts: []verdict{refused(pathAPI, apiMint), took(pathGossip)},
		},
		{
			name:     "mempool_conflict",
			verdicts: []verdict{took(pathBlock), refused(pathAPI, "couldn't issue tx: tx conflicts with other tx")},
		},
		{
			name:     "mempool_minted_meter",
			verdicts: []verdict{took(pathBlock), refused(pathAPI, "couldn't issue tx: failed to meter tx: AVAX minted")},
		},
		{
			name:     "mempool_not_enough_gas",
			verdicts: []verdict{took(pathBlock), refused(pathGossip, "not enough gas")},
		},
		{
			name:     "dropped_tx_cache",
			verdicts: []verdict{took(pathBlock), refused(pathAPI, "couldn't issue tx: failed execution: insufficient unlocked funds")},
		},
		{
			name:     "gas_capacity",
			verdicts: []verdict{took(pathBlock), refused(pathAPI, "couldn't issue tx: failed verification: tx exceeds current gas capacity: 9000 > 100")},
		},
		{
			name:     "later_chain_time_too_short",
			verdicts: []verdict{took(pathBlock), refused(pathAPI, "couldn't issue tx: failed verification: failed execution: staking period is too short")},
		},
		{
			name:     "later_wall_time_shared_memory",
			verdicts: []verdict{took(pathBlock), refused(pathGossip, "failed execution: failed to get shared memory: not found")},
		},
		{
			name:     "block_took_api_refused",
			verdicts: []verdict{took(pathBlock), refused(pathAPI, apiMint), took(pathGossip)},
			wantErr:  errPathsDisagree,
		},
		{
			name:     "block_took_gossip_refused",
			verdicts: []verdict{took(pathBlock), took(pathAPI), refused(pathGossip, gossipMint)},
			wantErr:  errPathsDisagree,
		},
		{
			name:     "api_took_gossip_refused",
			verdicts: []verdict{took(pathAPI), refused(pathGossip, gossipMint)},
			wantErr:  errPathsDisagree,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, judgePaths(tt.verdicts), tt.wantErr)
		})
	}
}

// TestJudgeBlocks feeds judgeBlocks node event lines. A case is judged once
// a node logs that it verified the case's block, and the block took the tx
// if any node verified it with no error.
func TestJudgeBlocks(t *testing.T) {
	const apiMint = "couldn't issue tx: failed verification: failed execution: insufficient unlocked funds"
	node, peer := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	agreed, disagreed, refused, waiting := ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID()
	cases := map[ids.ID]pathCase{
		agreed:    {tx: ids.GenerateTestID(), mempool: []verdict{{Path: pathAPI}}},
		disagreed: {tx: ids.GenerateTestID(), mempool: []verdict{{Path: pathAPI, Refusal: apiMint}}},
		refused:   {tx: ids.GenerateTestID(), mempool: []verdict{{Path: pathAPI}}},
		waiting:   {tx: ids.GenerateTestID(), mempool: []verdict{{Path: pathAPI, Refusal: apiMint}}},
	}

	var lines bytes.Buffer
	verifiedLine(&lines, node, peer, agreed, "")
	verifiedLine(&lines, node, peer, refused, "insufficient unlocked funds")
	judged, err := judgeBlocks(lines.Bytes(), cases)
	require.NoError(t, err)
	require.ElementsMatch(t, []ids.ID{agreed, refused}, judged)

	lines.Reset()
	verifiedLine(&lines, node, peer, disagreed, "insufficient unlocked funds")
	verifiedLine(&lines, peer, node, disagreed, "")
	_, err = judgeBlocks(lines.Bytes(), cases)
	require.ErrorIs(t, err, errPathsDisagree)
}

// verifiedLine writes the line of node's chain handler when the engine
// verified a block that a peer sent, and verification failed.
func verifiedLine(w *bytes.Buffer, node, from ids.NodeID, block ids.ID, verifyErr string) {
	type ref struct {
		Block  ids.ID `json:"block"`
		Height uint64 `json:"height"`
		Err    string `json:"err,omitempty"`
	}
	_, ev := eventlog.NewLogger(w).With("node", node).With("chain", "P").Start(context.Background(), "push_query")
	ev.Set("from", from)
	ev.Append("verified", ref{Block: block, Height: 5, Err: verifyErr})
	ev.End(nil)
}
