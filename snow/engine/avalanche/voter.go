// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Voter records chits received from [vdr] once its dependencies are met.
type voter struct {
	t         *Transitive
	vdr       ids.NodeID
	requestID uint32
	response  []ids.ID
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

	results := v.t.polls.Vote(v.requestID, v.vdr, v.response)
	if len(results) == 0 {
		return
	}

	previouslyLinearized, err := v.t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		v.t.errs.Add(err)
		return
	}

	for _, result := range results {
		result := result
		v.t.Ctx.Log.Debug("filtering poll results",
			zap.Stringer("result", &result),
		)

		_, err := v.bubbleVotes(ctx, result)
		if err != nil {
			v.t.errs.Add(err)
			return
		}

		v.t.Ctx.Log.Debug("finishing poll",
			zap.Stringer("result", &result),
		)
		if err := v.t.Consensus.RecordPoll(ctx, result); err != nil {
			v.t.errs.Add(err)
			return
		}
	}

	linearized, err := v.t.Manager.StopVertexAccepted(ctx)
	if err != nil {
		v.t.errs.Add(err)
		return
	}

	if linearized {
		// We guard here to ensure we only call the underlying vm.Linearize and
		// startSnowmanConsensus calls once.
		if !previouslyLinearized {
			// After the chain has been linearized, we will not be issuing any new
			// vertices.
			v.t.pendingTxs = nil
			v.t.metrics.pendingTxs.Set(0)

			// Invariant: The edge should only be the stop vertex after the
			// linearization.
			edge := v.t.Manager.Edge(ctx)
			stopVertexID := edge[0]
			if err := v.t.VM.Linearize(ctx, stopVertexID); err != nil {
				v.t.errs.Add(err)
				return
			}
			if err := v.t.startSnowmanConsensus(ctx, v.t.RequestID); err != nil {
				v.t.errs.Add(err)
			}
		}
		// If the chain has been linearized, there can't be any orphans, so we
		// can exit here.
		return
	}

	orphans := v.t.Consensus.Orphans()
	txs := make([]snowstorm.Tx, 0, orphans.Len())
	for orphanID := range orphans {
		if tx, err := v.t.VM.GetTx(ctx, orphanID); err == nil {
			txs = append(txs, tx)
		} else {
			v.t.Ctx.Log.Warn("failed to fetch tx during attempted re-issuance",
				zap.Stringer("txID", orphanID),
				zap.Error(err),
			)
		}
	}
	if len(txs) > 0 {
		v.t.Ctx.Log.Debug("re-issuing transactions",
			zap.Int("numTxs", len(txs)),
		)
	}
	if _, err := v.t.batch(ctx, txs, batchOption{force: true}); err != nil {
		v.t.errs.Add(err)
		return
	}

	if v.t.Consensus.Quiesce() {
		v.t.Ctx.Log.Debug("avalanche engine can quiesce")
		return
	}

	v.t.Ctx.Log.Debug("avalanche engine can't quiesce")
	v.t.repoll(ctx)
}

func (v *voter) bubbleVotes(ctx context.Context, votes bag.UniqueBag[ids.ID]) (bag.UniqueBag[ids.ID], error) {
	vertexHeap := vertex.NewHeap()
	for vote, set := range votes {
		vtx, err := v.t.Manager.GetVtx(ctx, vote)
		if err != nil {
			v.t.Ctx.Log.Debug("dropping vote(s)",
				zap.String("reason", "failed to fetch vertex"),
				zap.Stringer("voteID", vote),
				zap.Int("numVotes", set.Len()),
				zap.Error(err),
			)
			votes.RemoveSet(vote)
			continue
		}
		vertexHeap.Push(vtx)
	}

	for vertexHeap.Len() > 0 {
		vtx := vertexHeap.Pop()
		vtxID := vtx.ID()
		set := votes.GetSet(vtxID)
		status := vtx.Status()

		if !status.Fetched() {
			v.t.Ctx.Log.Debug("dropping vote(s)",
				zap.String("reason", "vertex unknown"),
				zap.Int("numVotes", set.Len()),
				zap.Stringer("vtxID", vtxID),
			)
			votes.RemoveSet(vtxID)
			continue
		}

		if status.Decided() {
			v.t.Ctx.Log.Verbo("dropping vote(s)",
				zap.String("reason", "vertex already decided"),
				zap.Int("numVotes", set.Len()),
				zap.Stringer("vtxID", vtxID),
				zap.Stringer("status", status),
			)

			votes.RemoveSet(vtxID)
			continue
		}

		if !v.t.Consensus.VertexIssued(vtx) {
			v.t.Ctx.Log.Verbo("bubbling vote(s)",
				zap.String("reason", "vertex not issued"),
				zap.Int("numVotes", set.Len()),
				zap.Stringer("vtxID", vtxID),
			)
			votes.RemoveSet(vtxID) // Remove votes for this vertex because it hasn't been issued

			parents, err := vtx.Parents()
			if err != nil {
				return votes, err
			}
			for _, parentVtx := range parents {
				votes.UnionSet(parentVtx.ID(), set)
				vertexHeap.Push(parentVtx)
			}
		}
	}

	return votes, nil
}
