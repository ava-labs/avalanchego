// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Voter records chits received from [vdr] once its dependencies are met.
type voter struct {
	t               *Transitive
	vdr             ids.NodeID
	requestID       uint32
	responseOptions []ids.ID
	deps            set.Set[ids.ID]
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

	var (
		vote       ids.ID
		shouldVote bool
		voteIndex  int
	)
	for i, voteOption := range v.responseOptions {
		// To prevent any potential deadlocks with undisclosed dependencies,
		// votes must be bubbled to the nearest valid block
		vote, shouldVote = v.getProcessingAncestor(ctx, voteOption)
		if shouldVote {
			voteIndex = i
			break
		}
	}

	var results []bag.Bag[ids.ID]
	if shouldVote {
		v.t.selectedVoteIndex.Observe(float64(voteIndex))
		results = v.t.polls.Vote(v.requestID, v.vdr, vote)
	} else {
		results = v.t.polls.Drop(v.requestID, v.vdr)
	}

	if len(results) == 0 {
		return
	}

	for _, result := range results {
		result := result
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

	if v.t.Consensus.NumProcessing() == 0 {
		v.t.Ctx.Log.Debug("Snowman engine can quiesce")
		return
	}

	v.t.Ctx.Log.Debug("Snowman engine can't quiesce")
	v.t.repoll(ctx)
}

// getProcessingAncestor finds [initialVote]'s most recent ancestor that is
// processing in consensus. If no ancestor could be found, false is returned.
//
// Note: If [initialVote] is processing, then [initialVote] will be returned.
func (v *voter) getProcessingAncestor(ctx context.Context, initialVote ids.ID) (ids.ID, bool) {
	// If [bubbledVote] != [initialVote], it is guaranteed that [bubbledVote] is
	// in processing. Otherwise, we attempt to iterate through any blocks we
	// have at our disposal as a best-effort mechanism to find a valid ancestor.
	bubbledVote := v.t.nonVerifieds.GetAncestor(initialVote)
	for {
		blk, err := v.t.GetBlock(ctx, bubbledVote)
		// If we cannot retrieve the block, drop [vote]
		if err != nil {
			v.t.Ctx.Log.Debug("dropping vote",
				zap.String("reason", "ancestor couldn't be fetched"),
				zap.Stringer("initialVoteID", initialVote),
				zap.Stringer("bubbledVoteID", bubbledVote),
				zap.Error(err),
			)
			v.t.numProcessingAncestorFetchesFailed.Inc()
			return ids.Empty, false
		}

		if v.t.Consensus.Decided(blk) {
			v.t.Ctx.Log.Debug("dropping vote",
				zap.String("reason", "bubbled vote already decided"),
				zap.Stringer("initialVoteID", initialVote),
				zap.Stringer("bubbledVoteID", bubbledVote),
				zap.Stringer("status", blk.Status()),
				zap.Uint64("height", blk.Height()),
			)
			v.t.numProcessingAncestorFetchesDropped.Inc()
			return ids.Empty, false
		}

		if v.t.Consensus.Processing(bubbledVote) {
			v.t.Ctx.Log.Verbo("applying vote",
				zap.Stringer("initialVoteID", initialVote),
				zap.Stringer("bubbledVoteID", bubbledVote),
				zap.Uint64("height", blk.Height()),
			)
			if bubbledVote != initialVote {
				v.t.numProcessingAncestorFetchesSucceeded.Inc()
			} else {
				v.t.numProcessingAncestorFetchesUnneeded.Inc()
			}
			return bubbledVote, true
		}

		bubbledVote = blk.Parent()
	}
}
