// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
)

// Voter records chits received from [vdr] once its dependencies are met.
type voter struct {
	t         *Transitive
	vdr       ids.ShortID
	requestID uint32
	response  []ids.ID
	deps      ids.Set
}

func (v *voter) Dependencies() ids.Set { return v.deps }

// Mark that a dependency has been met.
func (v *voter) Fulfill(id ids.ID) {
	v.deps.Remove(id)
	v.Update()
}

// Abandon this attempt to record chits.
func (v *voter) Abandon(id ids.ID) { v.Fulfill(id) }

func (v *voter) Update() {
	if v.deps.Len() != 0 || v.t.errs.Errored() {
		return
	}

	results := v.t.polls.Vote(v.requestID, v.vdr, v.response)
	if len(results) == 0 {
		return
	}
	for _, result := range results {
		_, err := v.bubbleVotes(result)
		if err != nil {
			v.t.errs.Add(err)
			return
		}
	}

	for _, result := range results {
		result := result

		v.t.Ctx.Log.Debug("Finishing poll with:\n%s", &result)
		if err := v.t.Consensus.RecordPoll(result); err != nil {
			v.t.errs.Add(err)
			return
		}
	}

	orphans := v.t.Consensus.Orphans()
	txs := make([]snowstorm.Tx, 0, orphans.Len())
	for orphanID := range orphans {
		if tx, err := v.t.VM.GetTx(orphanID); err == nil {
			txs = append(txs, tx)
		} else {
			v.t.Ctx.Log.Warn("Failed to fetch %s during attempted re-issuance", orphanID)
		}
	}
	if len(txs) > 0 {
		v.t.Ctx.Log.Debug("Re-issuing %d transactions", len(txs))
	}
	if _, err := v.t.batch(txs, true /*=force*/, false /*empty*/, false /*=limit*/); err != nil {
		v.t.errs.Add(err)
		return
	}

	if v.t.Consensus.Quiesce() {
		v.t.Ctx.Log.Debug("Avalanche engine can quiesce")
		return
	}

	v.t.Ctx.Log.Debug("Avalanche engine can't quiesce")
	v.t.repoll()
}

func (v *voter) bubbleVotes(votes ids.UniqueBag) (ids.UniqueBag, error) {
	vertexHeap := vertex.NewHeap()
	for vote, set := range votes {
		vtx, err := v.t.Manager.GetVtx(vote)
		if err != nil {
			v.t.Ctx.Log.Debug("Dropping %d vote(s) for %s because we failed to fetch the vertex",
				set.Len(), vote)
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
			v.t.Ctx.Log.Debug("Dropping %d vote(s) for %s because the vertex is unknown",
				set.Len(), vtxID)
			votes.RemoveSet(vtxID)
			continue
		}

		if status.Decided() {
			v.t.Ctx.Log.Verbo("Dropping %d vote(s) for %s because the vertex is decided as %s",
				set.Len(), vtxID, status)
			votes.RemoveSet(vtxID)
			continue
		}

		if !v.t.Consensus.VertexIssued(vtx) {
			v.t.Ctx.Log.Verbo("Bubbling %d vote(s) for %s because the vertex isn't issued",
				set.Len(), vtxID)
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
