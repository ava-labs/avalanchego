// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	// errPeerNotListed means that a node does not list an honest peer as
	// connected.
	errPeerNotListed = errors.New("honest peer is not listed")
	// errPeerACPs means that a node reports an honest peer's supported ACPs
	// as other than the ACPs the peer is configured to support.
	errPeerACPs = errors.New("peer's supported ACPs differ from its config")
)

// checkHandshakes fails t unless each honest node lists every other honest
// node as a peer, with the supported ACPs that the peer's config holds.
//
// Guarantee: a node records the ACPs that a peer's handshake says it supports,
// and info.peers reports them, limited to the ACPs that info.acps reports
// (api/info/service.md). Each node's handshake sends the ACPs of its config
// (network/network.go), and a node lists a peer only once the handshake is
// done. Source: info.peers and info.acps on each node, and the config the
// fuzzer started each node with. Links can still be coming up at the first
// check, so the check waits for a missing peer, up to a timeout; a listed peer
// must match at once.
func (r *runner) checkHandshakes(t *rapid.T) {
	const connectTimeout = 10 * time.Second

	ctx := t.Context()
	for i, uri := range r.uris {
		c := info.NewClient(uri)
		var acps info.ACPsReply
		require.NoError(t, c.Requester.SendRequest(ctx, "info.acps", struct{}{}, &acps))

		reportable := set.Of(slices.Collect(maps.Keys(acps.ACPs))...)
		want := maps.Clone(r.acps)
		delete(want, r.nodeIDs[i])

		// A node lists a peer before its chains log the peer's "connected"
		// line (network/network.go Connected). The RPCs take ctx, so the
		// bound ends the wait and never fails an RPC.
		waitCtx, cancel := context.WithTimeout(ctx, connectTimeout)
		var err error
		r.sinks[i].wait(waitCtx, func() bool {
			peers, peersErr := c.Peers(ctx, nil)
			require.NoError(t, peersErr)

			err = peerACPs(peers, want, reportable)
			return !errors.Is(err, errPeerNotListed)
		})
		cancel()
		if err != nil {
			r.fail(t, "handshake", fmt.Errorf("node %s: %w", r.nodeIDs[i], err))
		}
	}
}

// peerACPs returns an error unless peers, a node's info.peers reply, lists
// every node in want with the ACPs that want holds for it, limited to
// reportable. A mismatch is reported before a missing peer: a missing peer
// may still connect, and a mismatch never heals. Peers not in want, such as
// Byzantine ones, are not judged.
func peerACPs(peers []info.Peer, want map[ids.NodeID]set.Set[uint32], reportable set.Set[uint32]) error {
	listed := make(map[ids.NodeID]set.Set[uint32], len(peers))
	for _, p := range peers {
		listed[p.ID] = p.SupportedACPs
	}

	var missing []ids.NodeID
	for _, nodeID := range slices.SortedFunc(maps.Keys(want), ids.NodeID.Compare) {
		got, ok := listed[nodeID]
		if !ok {
			missing = append(missing, nodeID)
			continue
		}

		wantACPs := set.Intersect(want[nodeID], reportable)
		if !got.Equals(wantACPs) {
			return fmt.Errorf("%w: peer %s reports %v, want %v", errPeerACPs, nodeID, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(wantACPs)))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %v", errPeerNotListed, missing)
	}

	return nil
}
