// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/gecko/ids"
)

type voter struct {
	t         *Transitive
	vdr       ids.ShortID
	requestID uint32
	response  ids.ID
	deps      ids.Set
}

func (v *voter) Dependencies() ids.Set { return v.deps }

func (v *voter) Fulfill(id ids.ID) {
	v.deps.Remove(id)
	v.Update()
}

func (v *voter) Abandon(id ids.ID) { v.Fulfill(id) }

func (v *voter) Update() {
	if v.deps.Len() != 0 || v.t.errs.Errored() {
		return
	}

	results := ids.Bag{}
	finished := false
	if v.response.IsZero() {
		results, finished = v.t.polls.CancelVote(v.requestID, v.vdr)
	} else {
		results, finished = v.t.polls.Vote(v.requestID, v.vdr, v.response)
	}

	if !finished {
		return
	}

	// To prevent any potential deadlocks with un-disclosed dependencies, votes
	// must be bubbled to the nearest valid block
	results = v.bubbleVotes(results)

	v.t.Config.Context.Log.Debug("Finishing poll [%d] with:\n%s", v.requestID, &results)
	if err := v.t.Consensus.RecordPoll(results); err != nil {
		v.t.errs.Add(err)
		return
	}

	v.t.Config.VM.SetPreference(v.t.Consensus.Preference())

	if v.t.Consensus.Finalized() {
		v.t.Config.Context.Log.Debug("Snowman engine can quiesce")
		return
	}

	v.t.Config.Context.Log.Debug("Snowman engine can't quiesce")
	v.t.repoll()
}

func (v *voter) bubbleVotes(votes ids.Bag) ids.Bag {
	bubbledVotes := ids.Bag{}
	for _, vote := range votes.List() {
		count := votes.Count(vote)
		blk, err := v.t.Config.VM.GetBlock(vote)
		if err != nil {
			continue
		}

		for blk.Status().Fetched() && !v.t.Consensus.Issued(blk) {
			blk = blk.Parent()
		}

		if !blk.Status().Decided() && v.t.Consensus.Issued(blk) {
			bubbledVotes.AddCount(blk.ID(), count)
		}
	}
	return bubbledVotes
}
