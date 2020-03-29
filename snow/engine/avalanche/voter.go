// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

type voter struct {
	t         *Transitive
	vdr       ids.ShortID
	requestID uint32
	response  ids.Set
	deps      ids.Set
}

func (v *voter) Dependencies() ids.Set { return v.deps }

func (v *voter) Fulfill(id ids.ID) {
	v.deps.Remove(id)
	v.Update()
}

func (v *voter) Abandon(id ids.ID) { v.Fulfill(id) }

func (v *voter) Update() {
	if v.deps.Len() != 0 {
		return
	}

	results, finished := v.t.polls.Vote(v.requestID, v.vdr, v.response.List())
	if !finished {
		return
	}
	results = v.bubbleVotes(results)

	v.t.Config.Context.Log.Debug("Finishing poll with:\n%s", &results)
	v.t.Consensus.RecordPoll(results)

	txs := []snowstorm.Tx(nil)
	for _, orphanID := range v.t.Consensus.Orphans().List() {
		if tx, err := v.t.Config.VM.GetTx(orphanID); err == nil {
			txs = append(txs, tx)
		} else {
			v.t.Config.Context.Log.Warn("Failed to fetch %s during attempted re-issuance", orphanID)
		}
	}
	if len(txs) > 0 {
		v.t.Config.Context.Log.Debug("Re-issuing %d transactions", len(txs))
	}
	v.t.batch(txs, true /*=force*/, false /*empty*/)

	if v.t.Consensus.Quiesce() {
		v.t.Config.Context.Log.Verbo("Avalanche engine can quiesce")
		return
	}

	v.t.Config.Context.Log.Verbo("Avalanche engine can't quiesce")

	if len(v.t.polls.m) == 0 {
		v.t.repoll()
	}
}

func (v *voter) bubbleVotes(votes ids.UniqueBag) ids.UniqueBag {
	bubbledVotes := ids.UniqueBag{}
	for _, vote := range votes.List() {
		set := votes.GetSet(vote)
		vtx, err := v.t.Config.State.GetVertex(vote)
		if err != nil {
			continue
		}

		vts := []avalanche.Vertex{vtx}
		for len(vts) > 0 {
			vtx := vts[0]
			vts = vts[1:]

			if status := vtx.Status(); status.Fetched() && !v.t.Consensus.VertexIssued(vtx) {
				vts = append(vts, vtx.Parents()...)
			} else if !status.Decided() && v.t.Consensus.VertexIssued(vtx) {
				bubbledVotes.UnionSet(vtx.ID(), set)
			} else {
				v.t.Config.Context.Log.Debug("Dropping %d vote(s) for %s because the vertex is invalid", set.Len(), vtx.ID())
			}
		}
	}
	return bubbledVotes
}
