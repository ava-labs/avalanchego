// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"iter"
	"maps"
	"slices"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type networkedSUTs struct {
	validators, nonValidators map[ids.NodeID]*SUT
}

// newNetworkedSUTs creates a network of SUTs with the specified number of
// validators and non-validators.
//
// Like in production, all nodes are connected to all validators and mark
// themselves as connected. Although non-validators can connect to other
// non-validators, they do not generally attempt to do so in production, so this
// function does not connect them to each other.
func newNetworkedSUTs(tb testing.TB, numValidators, numNonValidators int) *networkedSUTs {
	tb.Helper()

	net := &networkedSUTs{
		validators:    make(map[ids.NodeID]*SUT, numValidators),
		nonValidators: make(map[ids.NodeID]*SUT, numNonValidators),
	}
	const numAccounts = 1
	for range numValidators {
		_, sut := newSUT(tb, numAccounts)
		net.validators[sut.nodeID()] = sut
	}
	for range numNonValidators {
		_, sut := newSUT(tb, numAccounts)
		net.nonValidators[sut.nodeID()] = sut
	}

	// To sanity check that the nodes agree on the genesis block, otherwise the
	// network will exhibit very weird behavior.
	_, expectedSUT := newSUT(tb, numAccounts)

	for selfID, sut := range net.allNodes() {
		require.Equalf(tb, expectedSUT.genesis.ID(), sut.genesis.ID(), "genesis ID for node %s", selfID)

		sut.validators.GetValidatorSetF = net.getValidatorSet

		s := sut.sender
		node := net.node(tb, selfID)
		s.SendAppRequestF = node.SendAppRequest
		s.SendAppResponseF = node.SendAppResponse
		s.SendAppErrorF = node.SendAppError
		s.SendAppGossipF = node.SendAppGossip

		// Connect all the peers _after_ setting up the sender functions.
		defer node.markConnectedToPeers(tb)
	}
	return net
}

func (net *networkedSUTs) allNodes() iter.Seq2[ids.NodeID, *SUT] {
	return func(yield func(ids.NodeID, *SUT) bool) {
		for id, sut := range net.validators {
			if !yield(id, sut) {
				return
			}
		}
		for id, sut := range net.nonValidators {
			if !yield(id, sut) {
				return
			}
		}
	}
}

func (net *networkedSUTs) allValidators() []*SUT {
	return slices.Collect(maps.Values(net.validators))
}

func (net *networkedSUTs) allNonValidators() []*SUT {
	return slices.Collect(maps.Values(net.nonValidators))
}

// getValidatorSet implements the [validators.State.GetValidatorSet] method,
// returning all validators in the network, each with weight 1.
func (net *networkedSUTs) getValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	vs := make(map[ids.NodeID]*validators.GetValidatorOutput)
	for id, sut := range net.validators {
		vs[id] = &validators.GetValidatorOutput{
			NodeID:    id,
			PublicKey: sut.rawVM.snowCtx.PublicKey,
			Weight:    1,
		}
	}
	return vs, nil
}

func (net *networkedSUTs) sutByID(tb testing.TB, id ids.NodeID) *SUT {
	tb.Helper()
	if sut, ok := net.validators[id]; ok {
		return sut
	}
	sut, ok := net.nonValidators[id]
	require.Truef(tb, ok, "Node %s is neither a validator nor non-validator", id)
	return sut
}

// A node couples an [SUT] with all of its peers as defined by a [networkedSUTs]
// instance.
type node struct {
	*SUT
	tb    testing.TB
	peers struct {
		all, validators, nonValidators map[ids.NodeID]*SUT
	}
}

func (net *networkedSUTs) node(tb testing.TB, selfID ids.NodeID) *node {
	tb.Helper()
	n := &node{
		SUT: net.sutByID(tb, selfID),
		tb:  tb,
	}
	n.peers.validators = net.validators

	n.peers.all = maps.Clone(net.validators)
	n.peers.all[selfID] = n.SUT
	// As in production, only validators connect to non-validators.
	if _, ok := net.validators[selfID]; ok {
		maps.Copy(n.peers.all, net.nonValidators)
		n.peers.nonValidators = net.nonValidators
	}
	return n
}

// peer returns the [SUT] with the given ID, asserting that it is in fact a peer
// of the current node. Requests for non-peer IDs will therefore result in a
// test failure but will allow the call site to continue if desired.
func (n *node) peer(id ids.NodeID, when string) (*SUT, bool) {
	if !assert.Containsf(n.tb, n.peers.all, id, "unknown peer %s in %s", id, when) {
		return nil, false
	}
	return n.peers.all[id], true
}

// Although [node] implements the [common.AppSender] interface, this is only to
// have local confirmation of matching signatures. Methods are actually accessed
// via [enginetest.Sender]s in the same goroutine. As a result, they deliver
// every message in a new goroutine to prevent re-entrant calls, which would
// result in a deadlock.
var _ common.AppSender = (*node)(nil)

func (n *node) SendAppRequest(ctx context.Context, to set.Set[ids.NodeID], requestID uint32, msg []byte) error {
	go func() {
		for peerID := range to {
			p, ok := n.peer(peerID, "SendAppRequest")
			if !ok {
				continue
			}
			assert.NoErrorf(n.tb, p.AppRequest(ctx, n.nodeID(), requestID, mockable.MaxTime, msg), "AppRequest(ctx, %s, ...)", peerID)
		}
	}()
	return nil
}

func (n *node) SendAppResponse(ctx context.Context, peerID ids.NodeID, requestID uint32, msg []byte) error {
	go func() {
		p, ok := n.peer(peerID, "SendAppResponse")
		if !ok {
			return
		}
		assert.NoErrorf(n.tb, p.AppResponse(ctx, n.nodeID(), requestID, msg), "AppResponse(ctx, %s, ...)", peerID)
	}()
	return nil
}

func (n *node) SendAppError(ctx context.Context, peerID ids.NodeID, requestID uint32, code int32, msg string) error {
	go func() {
		p, ok := n.peer(peerID, "SendAppError")
		if !ok {
			return
		}
		appErr := &common.AppError{
			Code:    code,
			Message: msg,
		}
		assert.NoErrorf(n.tb, p.AppRequestFailed(ctx, n.nodeID(), requestID, appErr), "AppRequestFailed(ctx, %s, ...)", peerID)
	}()
	return nil
}

func (n *node) SendAppGossip(ctx context.Context, to common.SendConfig, msg []byte) error {
	go func() {
		var sent set.Set[ids.NodeID]
		for peerID := range to.NodeIDs {
			p, ok := n.peer(peerID, "SendAppGossip")
			if !ok {
				return
			}
			assert.NoErrorf(n.tb, p.AppGossip(ctx, n.nodeID(), msg), "AppGossip(ctx, %s, ...)", peerID)
			sent.Add(peerID)
		}

		send := func(peers map[ids.NodeID]*SUT, count int) error {
			for peerID, peer := range peers {
				if count <= 0 {
					break
				}
				if sent.Contains(peerID) {
					continue
				}
				if err := peer.AppGossip(ctx, n.nodeID(), msg); err != nil {
					return err
				}

				sent.Add(peerID)
				count--
			}
			return nil
		}

		assert.NoError(n.tb, send(n.peers.validators, to.Validators), "sending to validators")
		assert.NoError(n.tb, send(n.peers.nonValidators, to.NonValidators), "sending to non-validators")
		assert.NoError(n.tb, send(n.peers.all, to.Peers), "sending to peers")
	}()
	return nil
}

func (n *node) markConnectedToPeers(tb testing.TB) {
	tb.Helper()
	for peerID := range n.peers.all {
		require.NoErrorf(tb, n.Connected(tb.Context(), peerID, version.Current), "%T.Connected(%s)", n.SUT, peerID)
	}
}
