// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"bytes"
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

// WarpACP118Peer is a test peer connection plus the deque of inbound P2P
// messages used by tmpnet warp e2e tests.
type WarpACP118Peer struct {
	Peer     peer.Peer
	Messages buffer.BlockingDeque[*message.InboundMessage]
}

type warpACP118AppResult struct {
	appBytes []byte
	appErr   *common.AppError
}

func parseWarpACP118AppReply(
	chainID ids.ID,
	requestID uint32,
) func(*message.InboundMessage) (*warpACP118AppResult, bool, error) {
	chainBytes := chainID[:]
	return func(msg *message.InboundMessage) (*warpACP118AppResult, bool, error) {
		switch m := msg.Message.(type) {
		case *p2ppb.AppResponse:
			if m.RequestId != requestID || !bytes.Equal(m.ChainId, chainBytes) {
				return nil, false, nil
			}
			return &warpACP118AppResult{appBytes: m.AppBytes}, true, nil
		case *p2ppb.AppError:
			if m.RequestId != requestID || !bytes.Equal(m.ChainId, chainBytes) {
				return nil, false, nil
			}
			return &warpACP118AppResult{
				appErr: &common.AppError{
					Code:    m.ErrorCode,
					Message: m.ErrorMessage,
				},
			}, true, nil
		default:
			return nil, false, nil
		}
	}
}

type warpACP118Sender struct {
	network *p2p.Network

	chainID ids.ID
	peers   map[ids.NodeID]WarpACP118Peer
	creator message.Creator
}

func (s *warpACP118Sender) SendAppRequest(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	requestID uint32,
	appRequestBytes []byte,
) error {
	if len(nodeIDs) != 1 {
		return fmt.Errorf("acp118 client: expected 1 node in SendAppRequest, got %d", len(nodeIDs))
	}
	var nodeID ids.NodeID
	for id := range nodeIDs {
		nodeID = id
		break
	}
	wp, ok := s.peers[nodeID]
	if !ok {
		return fmt.Errorf("acp118 client: no test peer for %s", nodeID)
	}
	outMsg, err := s.creator.AppRequest(s.chainID, requestID, e2e.DefaultWarpSignatureRequestTimeout, appRequestBytes)
	if err != nil {
		return err
	}
	if !wp.Peer.Send(ctx, outMsg) {
		return fmt.Errorf("acp118 client: Send failed for %s", nodeID)
	}
	go s.deliverAppReply(ctx, nodeID, requestID, wp)
	return nil
}

func (s *warpACP118Sender) deliverAppReply(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	wp WarpACP118Peer,
) {
	got, ok, err := e2e.FindP2PMessage(wp.Messages, parseWarpACP118AppReply(s.chainID, requestID))
	if err != nil || !ok {
		_ = s.network.AppRequestFailed(ctx, nodeID, requestID, common.ErrUndefined)
		return
	}
	if got.appErr != nil {
		_ = s.network.AppRequestFailed(ctx, nodeID, requestID, got.appErr)
		return
	}
	_ = s.network.AppResponse(ctx, nodeID, requestID, got.appBytes)
}

func (*warpACP118Sender) SendAppResponse(context.Context, ids.NodeID, uint32, []byte) error {
	return nil
}

func (*warpACP118Sender) SendAppError(context.Context, ids.NodeID, uint32, int32, string) error {
	return nil
}

func (*warpACP118Sender) SendAppGossip(context.Context, common.SendConfig, []byte) error {
	return nil
}

// NewWarpACP118SignatureAggregator builds an [acp118.SignatureAggregator] whose
// [p2p.Client] issues ACP-118 AppRequests over [peers] (tmpnet
// [peer.StartTestPeer] connections) and feeds AppResponses back into the
// internal p2p router.
func NewWarpACP118SignatureAggregator(
	log logging.Logger,
	chainID ids.ID,
	peers map[ids.NodeID]WarpACP118Peer,
) (*acp118.SignatureAggregator, error) {
	reg := prometheus.NewRegistry()
	msgCreator, err := message.NewCreator(
		reg,
		constants.DefaultNetworkCompressionType,
		e2e.DefaultWarpSignatureRequestTimeout,
	)
	if err != nil {
		return nil, err
	}

	sender := &warpACP118Sender{
		chainID: chainID,
		peers:   peers,
		creator: msgCreator,
	}
	net, err := p2p.NewNetwork(logging.NoLog{}, sender, reg, "warp_acp118_e2e")
	if err != nil {
		return nil, err
	}
	sender.network = net

	peerSet := &p2p.Peers{}
	for id := range peers {
		peerSet.Connected(id)
	}
	client := net.NewClient(p2p.SignatureRequestHandlerID, p2p.PeerSampler{Peers: peerSet})
	return acp118.NewSignatureAggregator(log, client), nil
}
