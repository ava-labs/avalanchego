// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// errLinkDropped means that an honest node lost its connection to another.
var errLinkDropped = errors.New("honest link dropped")

// flushNodeEvents returns the lines that the nodes logged since the last call,
// and appends each node's to its events.log (testNode.flushEvents).
func (r *runner) flushNodeEvents(t *rapid.T) []byte {
	var lines []byte
	for _, flush := range r.nodeEvents {
		b, err := flush()
		require.NoError(t, err)

		lines = append(lines, b...)
	}

	return lines
}

// checkNodeEvents runs the checks that read the lines of nodes' events.log
// since the last check: checkLinks, checkGossip, and checkPaths.
func (r *runner) checkNodeEvents(t *rapid.T, lines []byte) {
	r.checkLinks(t, lines)
	r.checkGossip(t, lines)
	r.checkPaths(t, lines)
}

// checkLinks fails t if a node logged in lines, which are lines of nodes'
// events.log, that it lost its connection to another honest node, unless a
// fault explains it (faults.linkExcused).
//
// Guarantee: honest links stay up while the fuzzer cuts none and delays none
// long enough to close one. Honest nodes never send a message that makes a
// peer close the link, and pings keep idle links inside the read deadline. A
// long delay or hold keeps pongs back and then lets them through in a burst,
// so a node closes the link itself, as it should.
// Source: each node's events.log, where the chain handler logs a
// "disconnected" event from the peer when the router reports the link down
// (snow/networking/router/chain_router.go Disconnected), and the fuzzer's
// fault spans and cuts. A Restart op must exempt the links it cuts.
//
// The check reads the logs only while the run lasts, so the links that close
// when cleanup stops the nodes do not count.
func (r *runner) checkLinks(t *rapid.T, lines []byte) {
	if err := judgeLinks(lines, set.Of(r.nodeIDs...), &r.faults); err != nil {
		r.fail(t, "honest links", err)
	}
}

// judgeLinks returns an error that names the first disconnect between two
// honest nodes in lines, which are lines of nodes' events.log, that f does
// not explain. A drop that f explains is what the fuzzer asked for.
//
// A benched peer also gets a "disconnected" event, with its link still up. The
// benchlist benches only a peer that fails most of its requests, which an
// honest peer on loopback does not unless a delay holds its replies past the
// request timeout, and only up to (1-α/K)/3 of the stake (config/config.go
// getBenchlistConfig). The benchlist reads the default K = 20 and α = 15, not
// the test network's, so that is a twelfth of the stake.
func judgeLinks(lines []byte, honest set.Set[ids.NodeID], f *faults) error {
	for line := range bytes.Lines(lines) {
		// Most lines are messages. Decode only the disconnects.
		if !bytes.Contains(line, []byte(`"op":"disconnected"`)) {
			continue
		}

		var ev struct {
			Timestamp int64      `json:"timestamp"`
			Node      ids.NodeID `json:"node"`
			Chain     string     `json:"chain"`
			Op        string     `json:"op"`
			From      ids.NodeID `json:"from"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return fmt.Errorf("decode event %q: %w", line, err)
		}
		if ev.Op != "disconnected" || !honest.Contains(ev.Node) || !honest.Contains(ev.From) {
			continue
		}

		at := time.Unix(0, ev.Timestamp)
		if !f.linkExcused(at, ev.Node, ev.From) {
			return fmt.Errorf("%w: node %s lost its connection to node %s (%s chain, timestamp %d)", errLinkDropped, ev.Node, ev.From, ev.Chain, ev.Timestamp)
		}
	}

	return nil
}

// A span is a time in which the network had a fault (faultSpan). A zero end
// means that it has not ended.
type span struct {
	start, end time.Time
}
