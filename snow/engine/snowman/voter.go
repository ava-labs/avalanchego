// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// Voter records chits received from [vdr] once its dependencies are met.
type voter struct {
	t         *Transitive
	vdr       ids.ShortID
	requestID uint32
	response  ids.ID
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

	var results []ids.Bag
	if v.response == ids.Empty {
		results = v.t.polls.Drop(v.requestID, v.vdr)
	} else {
		results = v.t.polls.Vote(v.requestID, v.vdr, v.response)
	}

	if len(results) == 0 {
		return
	}

	// To prevent any potential deadlocks with un-disclosed dependencies, votes
	// must be bubbled to the nearest valid block
	for i, result := range results {
		results[i] = v.bubbleVotes(result)
	}

	v.t.Ctx.Log.Debug("Finishing poll [%d] with:\n%s", v.requestID, &results)
	for _, result := range results {
		if err := v.t.Consensus.RecordPoll(result); err != nil {
			v.t.errs.Add(err)
		}
	}

	if v.t.errs.Errored() {
		return
	}

	if err := v.t.VM.SetPreference(v.t.Consensus.Preference()); err != nil {
		v.t.errs.Add(err)
		return
	}

	if v.t.Consensus.Finalized() {
		v.t.Ctx.Log.Debug("Snowman engine can quiesce")
		return
	}

	v.t.Ctx.Log.Debug("Snowman engine can't quiesce")
	v.t.repoll()
}

func (v *voter) bubbleVotes(votes ids.Bag) ids.Bag {
	bubbledVotes := ids.Bag{}
	for _, vote := range votes.List() {
		count := votes.Count(vote)
		blk, err := v.t.VM.GetBlock(vote)
		if err != nil {
			continue
		}

		status := blk.Status()
		blkID := blk.ID()
		for status.Fetched() && !v.t.Consensus.DecidedOrProcessing(blk) {
			blkID = blk.Parent()
			blk, err = v.t.GetBlock(blkID)
			if err != nil {
				status = choices.Unknown
				break
			}
			status = blk.Status()
		}

		if !status.Decided() && blk != nil && v.t.Consensus.DecidedOrProcessing(blk) {
			bubbledVotes.AddCount(blkID, count)
		}
	}
	return bubbledVotes
}
