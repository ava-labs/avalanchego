// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
)

var errStalled = errors.New("the nodes accepted no block while they had one to build")

// progress is the progress oracle (Stateful Fuzzing.md 3). It fails when the
// nodes' common height stays the same for a window, and the nodes show that
// they have a block to build at both ends of it. Two signs count: every node
// holds a tx that an op sent and no check decided (GetTxStatus reports
// Processing on every node), or a node built a P-Chain block above the height
// and then failed to verify it (the node's events.log). It reads the height
// from GetHeight, at each check.
//
// The guarantee: an honest node whose mempool is not empty builds in its own
// proposer slot. It includes the tx or a higher-paying one, or it drops the
// tx as invalid and stops holding it (vms/platformvm/block/builder/builder.go
// packEtnaBlockTxs, executeTx). A tx leaves a mempool with no drop only by a
// conflicting block, or by eviction from a full mempool, and both mean a
// block soon. Every node holds the tx, so the first honest slot builds. Only
// gas capacity can hold a build back, and the test network's never runs out
// (nodes.go setTestDefaults). A slot of the Byzantine validator builds
// nothing outside a propose op, so the window also covers a run of its slots
// (newRunner).
//
// A failed build counts because a build packs the mempool's txs and removes
// them (builder.go executeTx), so a block that then fails verification takes
// them out of every node that builds it, and no node holds the tx any more.
// The engine logs a block as built only when the VM returned one, and the VM
// returns one only when it has txs, a staker to reward, or a chain time to
// advance (builder.go buildBlock). The node builds and verifies the block on
// the same preferred state, in one handler call, so an honest node's build
// does not fail its own verification.
//
// Liveness holds only after GST (Stateful Fuzzing.md 3). The run is after GST
// except while a fault cuts an honest node off from the others, and a tail
// after it (faults.cutOff). The oracle judges no window that overlaps that
// time, so its window starts again once the network has settled. A node
// that keeps one clean link keeps the network live.
type progress struct {
	window time.Duration
	faults *faults
	height uint64
	since  time.Time // when the nodes were first seen at height with a block to build
}

// observe records that the nodes' common height is height at now, and
// pending, which says why the nodes have a block to build, or is empty if
// nothing shows that. It returns [errStalled] if the nodes were already at
// height a window ago, with a block to build then and now. Heights only grow,
// so the same height at both ends means no block in between. A time with
// nothing pending does not restart the window: a node can lose the tx for a
// moment and get it back by gossip. A window that p.faults excuses does.
func (p *progress) observe(now time.Time, height uint64, pending string) error {
	if height != p.height {
		p.height, p.since = height, time.Time{}
	}
	from := now
	if !p.since.IsZero() {
		from = p.since
	}
	if p.faults != nil && p.faults.cutOff(from, now) {
		p.since = time.Time{}
		return nil
	}

	if pending == "" {
		return nil
	}
	if p.since.IsZero() {
		p.since = now
		return nil
	}

	if stalled := now.Sub(p.since); stalled >= p.window {
		return fmt.Errorf("%w: the nodes stayed at height %d for %s, and %s", errStalled, height, stalled, pending)
	}

	return nil
}

// failedBuild returns a description of the first P-Chain block in lines,
// lines of nodes' events.log, that a node built above height and then failed
// to verify. It returns "" if there is none.
//
// The engine logs the build and the verification of the block it built in
// one event (snow/engine/snowman/engine.go buildBlocks,
// addUnverifiedBlockToConsensus). A failed verification of a peer's block does
// not count: it can fail for reasons of its own, such as a rejected parent.
func failedBuild(lines []byte, height uint64) (string, error) {
	type block struct {
		Block  ids.ID `json:"block"`
		Height uint64 `json:"height"`
		Err    string `json:"err"`
	}
	for line := range bytes.Lines(lines) {
		// Most lines are messages. Decode only the builds that failed.
		if !bytes.Contains(line, []byte(`"built":`)) || !bytes.Contains(line, []byte(`"err":`)) {
			continue
		}

		var ev struct {
			Node     ids.NodeID `json:"node"`
			Chain    string     `json:"chain"`
			Built    []block    `json:"built"`
			Verified []block    `json:"verified"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return "", fmt.Errorf("decode event %q: %w", line, err)
		}
		if ev.Chain != "P" {
			continue
		}

		for _, b := range ev.Built {
			for _, v := range ev.Verified {
				if v.Block == b.Block && v.Err != "" && b.Height > height {
					return fmt.Sprintf("node %s built block %s at height %d and failed to verify it: %s", ev.Node, b.Block, b.Height, v.Err), nil
				}
			}
		}
	}

	return "", nil
}

// checkProgress runs the progress oracle at a check: it observes the nodes'
// lowest height, and the work pending above it (pendingWork). held says
// whether every node held one same pending tx (decide), and lines are the
// lines of nodes' events.log since the last check.
func (r *runner) checkProgress(ctx context.Context, t *rapid.T, held bool, lines []byte) {
	height := r.minHeight(ctx, t)
	if err := r.progress.observe(time.Now(), height, pendingWork(t, held, height, lines)); err != nil {
		r.fail(t, "progress", err)
	}
}

// minHeight returns the lowest height that a node reports: the height up to
// which every node has accepted blocks.
func (r *runner) minHeight(ctx context.Context, t *rapid.T) uint64 {
	low, err := r.clients[0].GetHeight(ctx)
	require.NoError(t, err)
	for _, c := range r.clients[1:] {
		h, err := c.GetHeight(ctx)
		require.NoError(t, err)

		low = min(low, h)
	}

	return low
}

// pendingWork returns why the nodes have a block to build above height, or ""
// if nothing shows that: held, which says that every node holds a pending
// tx, or a node that logged in lines that it built a block above height and
// failed to verify it (failedBuild).
func pendingWork(t *rapid.T, held bool, height uint64, lines []byte) string {
	if held {
		return "every node held a pending tx"
	}

	failed, err := failedBuild(lines, height)
	require.NoError(t, err)

	return failed
}
