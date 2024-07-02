// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/job"
	"github.com/ava-labs/avalanchego/utils/bag"
)

var _ job.Job[ids.ID] = (*voter)(nil)

// Voter records chits received from [nodeID] once its dependencies are met.
type voter struct {
	t               *Transitive
	nodeID          ids.NodeID
	requestID       uint32
	responseOptions []ids.ID
}

// The resolution results from the dependencies of the voter aren't explicitly
// used. The responseOptions are used to determine which block to apply the vote
// to. The dependencies are only used to optimistically delay the application of
// the vote until the blocks have been issued.
func (v *voter) Execute(ctx context.Context, _ []ids.ID, _ []ids.ID) error {
	var (
		vote       ids.ID
		shouldVote bool
		voteIndex  int
	)
	for i, voteOption := range v.responseOptions {
		// To prevent any potential deadlocks with undisclosed dependencies,
		// votes must be bubbled to the nearest valid block
		vote, shouldVote = v.t.getProcessingAncestor(ctx, voteOption)
		if shouldVote {
			voteIndex = i
			break
		}
	}

	var results []bag.Bag[ids.ID]
	if shouldVote {
		v.t.selectedVoteIndex.Observe(float64(voteIndex))
		results = v.t.polls.Vote(v.requestID, v.nodeID, vote)
	} else {
		results = v.t.polls.Drop(v.requestID, v.nodeID)
	}

	if len(results) == 0 {
		return nil
	}

	for _, result := range results {
		result := result
		v.t.Ctx.Log.Debug("finishing poll",
			zap.Stringer("result", &result),
		)
		if err := v.t.Consensus.RecordPoll(ctx, result); err != nil {
			return err
		}
	}

	if err := v.t.VM.SetPreference(ctx, v.t.Consensus.Preference()); err != nil {
		return err
	}

	if v.t.Consensus.NumProcessing() == 0 {
		v.t.Ctx.Log.Debug("Snowman engine can quiesce")
		return nil
	}

	v.t.Ctx.Log.Debug("Snowman engine can't quiesce")
	v.t.repoll(ctx)
	return nil
}
