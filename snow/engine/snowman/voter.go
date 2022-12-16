// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Voter records chits received from [vdr] once its dependencies are met.
type voter struct {
	t         *Transitive
	vdr       ids.NodeID
	requestID uint32
	response  ids.ID
	deps      set.Set[ids.ID]
}

func (v *voter) Dependencies() set.Set[ids.ID] {
	return v.deps
}

// Mark that a dependency has been met.
func (v *voter) Fulfill(ctx context.Context, id ids.ID) {
	v.deps.Remove(id)
	v.Update(ctx)
}

// Abandon this attempt to record chits.
func (v *voter) Abandon(ctx context.Context, id ids.ID) {
	v.Fulfill(ctx, id)
}

func (v *voter) Update(ctx context.Context) {
	if v.deps.Len() != 0 || v.t.errs.Errored() {
		return
	}

	var results []ids.Bag
	if v.response == ids.Empty {
		results = v.t.polls.Drop(v.requestID, v.vdr)
	} else {
		results = v.t.polls.Vote(v.requestID, v.vdr, v.response)
	}

	if len(results) == 0 {
		return
	}

	for _, result := range results {
		result := result
		v.t.Ctx.Log.Debug("filtering poll results",
			zap.Stringer("result", &result),
		)

		// To prevent any potential deadlocks with un-disclosed dependencies,
		// votes must be bubbled to the nearest valid block
		result = v.bubbleVotes(ctx, result)
		v.t.Ctx.Log.Debug("finishing poll",
			zap.Stringer("result", &result),
		)
		if err := v.t.Consensus.RecordPoll(ctx, result); err != nil {
			v.t.errs.Add(err)
		}
	}

	if v.t.errs.Errored() {
		return
	}

	if err := v.t.VM.SetPreference(ctx, v.t.Consensus.Preference()); err != nil {
		v.t.errs.Add(err)
		return
	}

	if v.t.Consensus.Finalized() {
		v.t.Ctx.Log.Debug("Snowman engine can quiesce")
		return
	}

	v.t.Ctx.Log.Debug("Snowman engine can't quiesce")
	v.t.repoll(ctx)
}

// bubbleVotes bubbles the [votes] a set of the number of votes for specific
// blkIDs that received votes in consensus, to their most recent ancestor that
// has been issued to consensus.
//
// Note: bubbleVotes does not bubbleVotes to all of the ancestors in consensus,
// just the most recent one. bubbling to the rest of the ancestors, which may
// also be in consensus is handled in RecordPoll.
func (v *voter) bubbleVotes(ctx context.Context, votes ids.Bag) ids.Bag {
	bubbledVotes := ids.Bag{}

votesLoop:
	for _, vote := range votes.List() {
		count := votes.Count(vote)
		// use rootID in case of this is a non-verified block ID
		rootID := v.t.nonVerifieds.GetRoot(vote)
		v.t.Ctx.Log.Verbo("bubbling vote(s) through unverified blocks",
			zap.Int("numVotes", count),
			zap.Stringer("voteID", vote),
			zap.Stringer("parentID", rootID),
		)

		blk, err := v.t.GetBlock(ctx, rootID)
		// If we cannot retrieve the block, drop [vote]
		if err != nil {
			v.t.Ctx.Log.Debug("dropping vote(s)",
				zap.String("reason", "parent couldn't be fetched"),
				zap.Stringer("parentID", rootID),
				zap.Int("numVotes", count),
				zap.Stringer("voteID", vote),
				zap.Error(err),
			)
			continue
		}

		status := blk.Status()
		blkID := blk.ID()
		// If we have not fetched [blkID] break from the loop. We will drop the
		// vote below and move on to the next vote.
		//
		// If [blk] has already been decided, break from the loop, we will drop
		// the vote below since there is no need to count the votes for a [blk]
		// we've already finalized.
		//
		// If [blk] is currently in consensus, break from the loop, we have
		// reached the first ancestor of the original [vote] that has been
		// issued consensus. In this case, the votes will be bubbled further
		// from [blk] to any of its ancestors that are also in consensus.
		for status.Fetched() && !(v.t.Consensus.Decided(blk) || v.t.Consensus.Processing(blkID)) {
			parentID := blk.Parent()
			v.t.Ctx.Log.Verbo("pushing vote(s)",
				zap.Int("numVotes", count),
				zap.Stringer("voteID", vote),
				zap.Stringer("parentID", rootID),
			)

			blkID = parentID
			blk, err = v.t.GetBlock(ctx, blkID)
			// If we cannot retrieve the block, drop [vote]
			if err != nil {
				v.t.Ctx.Log.Debug("dropping vote(s)",
					zap.String("reason", "block couldn't be fetched"),
					zap.Stringer("blkID", blkID),
					zap.Int("numVotes", count),
					zap.Stringer("voteID", vote),
					zap.Error(err),
				)
				continue votesLoop
			}
			status = blk.Status()
		}

		// If [blkID] is currently in consensus, count the votes
		if v.t.Consensus.Processing(blkID) {
			v.t.Ctx.Log.Verbo("applying vote(s)",
				zap.Int("numVotes", count),
				zap.Stringer("blkID", blkID),
				zap.Stringer("status", status),
			)
			bubbledVotes.AddCount(blkID, count)
		} else {
			v.t.Ctx.Log.Verbo("dropping vote(s)",
				zap.Int("numVotes", count),
				zap.Stringer("blkID", blkID),
				zap.Stringer("status", status),
			)
		}
	}
	return bubbledVotes
}
