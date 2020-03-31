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
	if v.deps.Len() != 0 {
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

	v.t.Config.Context.Log.Verbo("Finishing poll [%d] with:\n%s", v.requestID, &results)
	v.t.Consensus.RecordPoll(results)

	v.t.Config.VM.SetPreference(v.t.Consensus.Preference())

	if v.t.Consensus.Finalized() {
		v.t.Config.Context.Log.Verbo("Snowman engine can quiesce")
		return
	}

	v.t.Config.Context.Log.Verbo("Snowman engine can't quiesce")

	if len(v.t.polls.m) < v.t.Config.Params.ConcurrentRepolls {
		v.t.repoll()
	}
}
