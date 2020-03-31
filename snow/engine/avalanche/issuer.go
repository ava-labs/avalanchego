// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
)

type issuer struct {
	t                 *Transitive
	vtx               avalanche.Vertex
	issued, abandoned bool
	vtxDeps, txDeps   ids.Set
}

func (i *issuer) FulfillVtx(id ids.ID) {
	i.vtxDeps.Remove(id)
	i.Update()
}

func (i *issuer) FulfillTx(id ids.ID) {
	i.txDeps.Remove(id)
	i.Update()
}

func (i *issuer) Abandon() {
	if !i.abandoned {
		vtxID := i.vtx.ID()
		i.t.pending.Remove(vtxID)
		i.abandoned = true

		i.t.vtxBlocked.Abandon(vtxID)
	}
}

func (i *issuer) Update() {
	if i.abandoned || i.issued || i.vtxDeps.Len() != 0 || i.txDeps.Len() != 0 || i.t.Consensus.VertexIssued(i.vtx) {
		return
	}
	i.issued = true

	vtxID := i.vtx.ID()
	i.t.pending.Remove(vtxID)

	for _, tx := range i.vtx.Txs() {
		if err := tx.Verify(); err != nil {
			i.t.Config.Context.Log.Debug("Transaction failed verification due to %s, dropping vertex", err)
			i.t.vtxBlocked.Abandon(vtxID)
			return
		}
	}

	i.t.Config.Context.Log.Verbo("Adding vertex to consensus:\n%s", i.vtx)

	i.t.Consensus.Add(i.vtx)

	p := i.t.Consensus.Parameters()
	vdrs := i.t.Config.Validators.Sample(p.K) // Validators to sample

	vdrSet := ids.ShortSet{} // Validators to sample repr. as a set
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	i.t.RequestID++
	polled := false
	if numVdrs := len(vdrs); numVdrs == p.K && i.t.polls.Add(i.t.RequestID, vdrSet.Len()) {
		i.t.Config.Sender.PushQuery(vdrSet, i.t.RequestID, vtxID, i.vtx.Bytes())
		polled = true
	} else if numVdrs < p.K {
		i.t.Config.Context.Log.Error("Query for %s was dropped due to an insufficient number of validators", vtxID)
	}

	i.t.vtxBlocked.Fulfill(vtxID)
	for _, tx := range i.vtx.Txs() {
		i.t.txBlocked.Fulfill(tx.ID())
	}

	if polled && len(i.t.polls.m) < i.t.Params.ConcurrentRepolls {
		i.t.repoll()
	}
}

type vtxIssuer struct{ i *issuer }

func (vi *vtxIssuer) Dependencies() ids.Set { return vi.i.vtxDeps }
func (vi *vtxIssuer) Fulfill(id ids.ID)     { vi.i.FulfillVtx(id) }
func (vi *vtxIssuer) Abandon(ids.ID)        { vi.i.Abandon() }
func (vi *vtxIssuer) Update()               { vi.i.Update() }

type txIssuer struct{ i *issuer }

func (ti *txIssuer) Dependencies() ids.Set { return ti.i.txDeps }
func (ti *txIssuer) Fulfill(id ids.ID)     { ti.i.FulfillTx(id) }
func (ti *txIssuer) Abandon(ids.ID)        { ti.i.Abandon() }
func (ti *txIssuer) Update()               { ti.i.Update() }
