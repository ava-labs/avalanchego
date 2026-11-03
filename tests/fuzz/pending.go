// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"slices"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

// A pendingTx is a tx that the runner sent and no check decided yet. An op
// does not wait for its tx: the block scan decides that it committed, and the
// nodes' statuses that it was dropped (decide).
type pendingTx struct {
	id ids.ID
	// tx is the P-Chain tx, or nil for an export from the X-Chain, which
	// node 0's X-Chain decides.
	tx *platform.Tx
	// op names the op in the outcome counts, or is "" for a tx of a proposed
	// block, whose op counts the block.
	op   string
	step int    // the op that sent it
	path string // the path it came in on
	// tally is the tx type under which checkCommits counts the attempt, or
	// "" if the attempt is not fair: mutated, or with a reference that is no
	// active entity of its kind.
	tally string
	sent  time.Time
	// polled is when the last check found the tx still pending, or sent
	// before one did. A drop came after it.
	polled time.Time
}

// track keeps p pending until a check decides it. Its inputs are hidden from
// the builder until then (builder).
func (r *runner) track(p *pendingTx) {
	if p.tx != nil {
		p.id = p.tx.ID()
	}
	p.polled = p.sent
	r.pending = append(r.pending, p)
}

// pendingInputs returns the input IDs that the pending P-Chain txs spend.
func (r *runner) pendingInputs() set.Set[ids.ID] {
	var inputs set.Set[ids.ID]
	for _, p := range r.pending {
		if p.tx != nil {
			inputs.Union(p.tx.InputIDs())
		}
	}

	return inputs
}

// pendingStakers returns the node IDs of the validators that pending txs add.
func (r *runner) pendingStakers() set.Set[ids.NodeID] {
	var nodeIDs set.Set[ids.NodeID]
	for _, p := range r.pending {
		if p.tx == nil {
			continue
		}
		if s, ok := p.tx.Unsigned.(interface{ NodeID() ids.NodeID }); ok {
			nodeIDs.Add(s.NodeID())
		}
	}

	return nodeIDs
}

// resolve records that p ended in result, decided by reason ("" for none),
// and stops tracking it. A fair attempt with a verdict counts toward
// checkCommits (tally).
func (r *runner) resolve(t *rapid.T, p *pendingTx, result outcome, reason string) {
	r.pending = slices.DeleteFunc(r.pending, func(q *pendingTx) bool { return q == p })
	if p.op == "" {
		return
	}

	_, ev := r.events.Start(t.Context(), "decided")
	defer ev.End(nil)
	ev.Set("step", p.step)
	ev.Set("tx", p.id)
	ev.Set("outcome", result.String())
	if reason != "" {
		ev.Set("decided_by", p.path+": "+reason)
		reason = p.path + ": " + countable(reason)
	}

	t.Logf("op %d %s: tx %s %s %s", p.step, p.op, p.id, result, reason)
	r.record(p.op, result, reason)
	if p.tally == "" || result == undecided {
		return
	}

	ev.Set("fair", r.tally(p.tally, result, reason, p.polled, time.Now()))
	if result == dropped {
		if r.fairDrops == nil {
			r.fairDrops = make(map[ids.ID]*pendingTx)
		}
		r.fairDrops[p.id] = p
	}
}

// resolveCommitted resolves the pending tx txID, if there is one, as
// committed. The block scan calls it for each accepted tx. A fair attempt
// that a check decided dropped counts its commit too: txVerdict can decide a
// drop while a node holds the tx in a block off its preferred chain.
func (r *runner) resolveCommitted(t *rapid.T, txID ids.ID) {
	if i := slices.IndexFunc(r.pending, func(p *pendingTx) bool { return p.id == txID }); i >= 0 {
		r.resolve(t, r.pending[i], committed, "")
		return
	}
	p, ok := r.fairDrops[txID]
	if !ok {
		return
	}

	delete(r.fairDrops, txID)
	_, ev := r.events.Start(t.Context(), "decided")
	defer ev.End(nil)
	ev.Set("step", p.step)
	ev.Set("tx", p.id)
	ev.Set("outcome", committed.String())
	ev.Set("after_dropped", true)
	t.Logf("op %d %s: tx %s committed after a check decided it dropped", p.step, p.op, p.id)
	now := time.Now()
	ev.Set("fair", r.tally(p.tally, committed, "", now, now))
}

// decide asks the nodes about each pending tx that the block scan has not
// committed. A P-Chain tx is dropped once txVerdict says so; an export from
// the X-Chain commits once node 0's X-Chain accepts it. A tx that waited
// longer than pendingTimeout is undecided, and the checks stop asking about
// it; it can still commit, and the block scan learns it then. decide returns
// whether every node held one same pending tx, for the progress oracle.
func (r *runner) decide(ctx context.Context, t *rapid.T) bool {
	// It is not a liveness bound: a node that alone holds a tx waits for its
	// own proposer slot, up to proposer.MaxLookAheadSlots windows away, and
	// the progress oracle judges liveness.
	const pendingTimeout = 30 * time.Second

	var held bool
	for _, p := range slices.Clone(r.pending) {
		if p.tx == nil {
			s, err := r.xClient.GetTxStatus(ctx, p.id)
			require.NoError(t, err)
			if s == choices.Accepted {
				r.resolve(t, p, committed, "")
				continue
			}
		} else {
			statuses := make([]*platformvm.GetTxStatusResponse, len(r.clients))
			for i, c := range r.clients {
				var err error
				statuses[i], err = c.GetTxStatus(ctx, p.id)
				require.NoError(t, err)
			}
			drop, reason, all := txVerdict(statuses)
			if drop {
				r.resolve(t, p, dropped, reason)
				continue
			}
			held = held || all
		}

		if time.Since(p.sent) > pendingTimeout {
			r.resolve(t, p, undecided, "")
			continue
		}
		p.polled = time.Now()
	}

	return held
}

// txVerdict returns how statuses, a pending tx's status on every node, decide
// it: dropped, with the reason, if a node dropped it and no node holds it or
// committed it. A node that holds it can still build it, and a committed tx
// is the block scan's to decide. A node that holds it only in a block off its
// preferred chain reports Unknown (vms/platformvm/service.go GetTxStatus), so
// a dropped tx can still commit (resolveCommitted). held reports whether
// every node holds it, in its mempool or in a block not yet decided.
func txVerdict(statuses []*platformvm.GetTxStatusResponse) (dropped bool, reason string, held bool) {
	held = true
	var live bool
	for _, s := range statuses {
		switch s.Status {
		case status.Dropped, status.Aborted:
			dropped = true
			if reason == "" {
				reason = s.Reason
			}
		case status.Processing, status.Committed:
			live = true
		}
		held = held && s.Status == status.Processing
	}
	if live {
		return false, "", held
	}

	return dropped, reason, held
}
