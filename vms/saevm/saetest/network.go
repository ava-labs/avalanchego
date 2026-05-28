// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"context"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/libevm/eventual"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

// Peer defines the minimal surface for using the [Sender] and [Connect]
// helpers.
type Peer interface {
	common.AppHandler
	validators.Connector
	NodeID() ids.NodeID
	Sender() *Sender
}

var _ common.AppSender = (*Sender)(nil)

// Sender is a test [common.AppSender] that routes messages between in-process
// peers registered via [Sender.AddPeer]. Like the production avalanchego
// instance, each call is delivered in its own goroutine.
type Sender struct {
	tb   testing.TB
	vdrs set.Set[ids.NodeID]

	self   eventual.Value[common.AppHandler]
	selfID ids.NodeID

	peersLock sync.RWMutex
	peers     map[ids.NodeID]common.AppHandler
}

// NewSender returns a [Sender] whose validator-set sampling is driven by vdrs.
func NewSender(tb testing.TB, vdrs set.Set[ids.NodeID]) *Sender {
	return &Sender{
		tb:    tb,
		self:  eventual.New[common.AppHandler](),
		vdrs:  vdrs,
		peers: make(map[ids.NodeID]common.AppHandler),
	}
}

// SetSelf binds the sender to the local node. It MUST be called before any
// other peer's handler is invoked, since [Sender] uses self's NodeID as the
// source of every routed message.
func (s *Sender) SetSelf(self Peer) {
	s.selfID = self.NodeID()
	s.self.Put(self)
}

// AddPeer registers peer so that messages addressed to peer.NodeID() are
// delivered to it.
func (s *Sender) AddPeer(peer Peer) {
	s.peersLock.Lock()
	defer s.peersLock.Unlock()

	s.peers[peer.NodeID()] = peer
}

func (s *Sender) SendAppRequest(_ context.Context, to set.Set[ids.NodeID], requestID uint32, b []byte) error {
	go s.sendAppRequest(to, requestID, b)
	return nil
}

func (s *Sender) SendAppResponse(_ context.Context, to ids.NodeID, requestID uint32, b []byte) error {
	go s.sendAppResponse(to, requestID, b)
	return nil
}

func (s *Sender) SendAppError(_ context.Context, nodeID ids.NodeID, requestID uint32, code int32, message string) error {
	go s.sendAppError(nodeID, requestID, code, message)
	return nil
}

func (s *Sender) SendAppGossip(_ context.Context, c common.SendConfig, b []byte) error {
	go s.sendAppGossip(c, b)
	return nil
}

func (s *Sender) sendAppRequest(to set.Set[ids.NodeID], requestID uint32, b []byte) {
	ctx := s.tb.Context()
	self, selfID := s.getSelf()
	for peerID := range to {
		if peer, ok := s.getPeer(peerID); ok {
			assert.NoErrorf(s.tb, peer.AppRequest(ctx, selfID, requestID, mockable.MaxTime, b), "%T.AppRequest(%s)", peer, selfID)
		} else {
			assert.NoErrorf(s.tb, self.AppRequestFailed(ctx, peerID, requestID, common.ErrTimeout), "%T.AppRequestFailed(%s)", self, peerID)
		}
	}
}

func (s *Sender) sendAppResponse(to ids.NodeID, requestID uint32, b []byte) {
	peer, ok := s.getPeer(to)
	if !ok {
		return
	}
	ctx := s.tb.Context()
	_, selfID := s.getSelf()
	assert.NoErrorf(s.tb, peer.AppResponse(ctx, selfID, requestID, b), "%T.AppResponse(%s)", peer, selfID)
}

func (s *Sender) sendAppError(to ids.NodeID, requestID uint32, code int32, message string) {
	peer, ok := s.getPeer(to)
	if !ok {
		return
	}
	ctx := s.tb.Context()
	_, selfID := s.getSelf()
	appErr := &common.AppError{
		Code:    code,
		Message: message,
	}
	assert.NoErrorf(s.tb, peer.AppRequestFailed(ctx, selfID, requestID, appErr), "%T.AppRequestFailed(%s)", peer, selfID)
}

func (s *Sender) sendAppGossip(c common.SendConfig, b []byte) {
	var (
		ctx       = s.tb.Context()
		_, selfID = s.getSelf()
	)
	for _, peer := range s.sample(c) {
		assert.NoErrorf(s.tb, peer.AppGossip(ctx, selfID, b), "%T.AppGossip(%s)", peer, selfID)
	}
}

func (s *Sender) sample(c common.SendConfig) []common.AppHandler {
	var (
		sent   set.Set[ids.NodeID]
		toSend []common.AppHandler
	)
	if self, selfID := s.getSelf(); c.NodeIDs.Contains(selfID) {
		sent.Add(selfID)
		toSend = append(toSend, self)
	}

	s.peersLock.RLock()
	defer s.peersLock.RUnlock()

	add := func(count int, allow func(peerID ids.NodeID) bool) {
		for peerID, peer := range s.peers {
			if count <= 0 {
				break
			}
			if !sent.Contains(peerID) && allow(peerID) {
				sent.Add(peerID)
				toSend = append(toSend, peer)
				count--
			}
		}
	}
	add(c.NodeIDs.Len(), c.NodeIDs.Contains)
	add(c.Validators, s.vdrs.Contains)
	add(c.NonValidators, func(peerID ids.NodeID) bool {
		return !s.vdrs.Contains(peerID)
	})
	add(c.Peers, func(ids.NodeID) bool { return true })
	return toSend
}

func (s *Sender) getSelf() (common.AppHandler, ids.NodeID) {
	self := s.self.Peek() // ensure SetSelf is called before accessing selfID
	return self, s.selfID
}

func (s *Sender) getPeer(peerID ids.NodeID) (common.AppHandler, bool) {
	if self, selfID := s.getSelf(); selfID == peerID {
		return self, true
	}

	s.peersLock.RLock()
	defer s.peersLock.RUnlock()

	peer, ok := s.peers[peerID]
	return peer, ok
}

// Connect wires every given pair of peers together, marking both sides as
// connected.
func Connect[P Peer](tb testing.TB, peers ...P) {
	tb.Helper()

	for i, peer := range peers {
		ConnectTo(tb, peer, peers[:i]...)
	}
}

// ConnectTo wires self to each peer, marking both sides as connected.
func ConnectTo[P Peer](tb testing.TB, self P, peers ...P) {
	tb.Helper()

	ctx := tb.Context()
	var (
		selfID     = self.NodeID()
		selfSender = self.Sender()
	)
	for _, peer := range peers {
		selfSender.AddPeer(peer)
		peer.Sender().AddPeer(self)

		dstID := peer.NodeID()
		require.NoErrorf(tb, self.Connected(ctx, dstID, version.Current), "%T.Connected(%s)", self, dstID)
		require.NoErrorf(tb, peer.Connected(ctx, selfID, version.Current), "%T.Connected(%s)", peer, selfID)
	}
}
