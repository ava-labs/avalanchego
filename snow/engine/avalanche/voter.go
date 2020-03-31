// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/gecko/ids"
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

	if len(v.t.polls.m) < v.t.Config.Params.ConcurrentRepolls {
		v.t.repoll()
	}
}
