// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var errInvalidWarpSignature = errors.New("invalid warp signature")

// AggregateWarpSignature requests an ACP-118 signature for msg from every
// validator in vdrs and combines the responses into a signed warp message.
// Every validator MUST be a node of network.
func AggregateWarpSignature(
	ctx context.Context,
	network *tmpnet.Network,
	vdrs validators.WarpSet,
	msg *warp.UnsignedMessage,
) (*warp.Message, error) {
	nodes := make(map[ids.NodeID]*tmpnet.Node, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.NodeID] = node
	}

	signers := set.NewBits()
	sigs := make([]*bls.Signature, 0, len(vdrs.Validators))
	for i, vdr := range vdrs.Validators {
		nodeID := vdr.NodeIDs[0]
		node, ok := nodes[nodeID]
		if !ok {
			return nil, fmt.Errorf("validator %s is not a node of the network", nodeID)
		}

		sig, err := requestWarpSignature(ctx, network.GetNetworkID(), node, msg)
		if err != nil {
			return nil, fmt.Errorf("requesting signature from %s: %w", nodeID, err)
		}
		if !bls.Verify(vdr.PublicKey, sig, msg.Bytes()) {
			return nil, fmt.Errorf("%w from %s", errInvalidWarpSignature, nodeID)
		}
		signers.Add(i)
		sigs = append(sigs, sig)
	}

	aggSig, err := bls.AggregateSignatures(sigs)
	if err != nil {
		return nil, fmt.Errorf("aggregating signatures: %w", err)
	}
	sig := &warp.BitSetSignature{Signers: signers.Bytes()}
	copy(sig.Signature[:], bls.SignatureToBytes(aggSig))
	return warp.NewMessage(msg, sig)
}

// requestWarpSignature connects to node as a peer and requests its ACP-118
// signature for msg.
func requestWarpSignature(
	ctx context.Context,
	networkID uint32,
	node *tmpnet.Node,
	msg *warp.UnsignedMessage,
) (*bls.Signature, error) {
	stakingAddress, cancel, err := node.GetAccessibleStakingAddress(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting staking address: %w", err)
	}
	defer cancel()

	// The node also sends handshake and gossip messages; only the response to
	// the signature request is of interest.
	responses := make(chan *message.InboundMessage, 1)
	testPeer, err := peer.StartTestPeer(
		ctx,
		stakingAddress,
		networkID,
		router.InboundHandlerFunc(func(_ context.Context, m *message.InboundMessage) {
			switch m.Message.(type) {
			case *p2ppb.AppResponse, *p2ppb.AppError:
				select {
				case responses <- m:
				default:
				}
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("starting test peer: %w", err)
	}
	defer func() {
		testPeer.StartClose()
		_ = testPeer.AwaitClosed(ctx)
	}()

	request, err := newWarpSignatureRequest(msg)
	if err != nil {
		return nil, fmt.Errorf("creating signature request: %w", err)
	}
	if !testPeer.Send(ctx, request) {
		return nil, errors.New("sending signature request")
	}

	select {
	case m := <-responses:
		return parseWarpSignatureResponse(m)
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for signature response: %w", ctx.Err())
	}
}

func newWarpSignatureRequest(msg *warp.UnsignedMessage) (*message.OutboundMessage, error) {
	creator, err := message.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		e2e.DefaultTimeout,
	)
	if err != nil {
		return nil, err
	}

	requestBytes, err := proto.Marshal(&sdk.SignatureRequest{
		Message: msg.Bytes(),
	})
	if err != nil {
		return nil, err
	}

	// The SDK network uses odd request IDs. Coreth and Subnet-EVM route even
	// request IDs to their own protocol, which does not answer this request.
	const requestID = 1
	return creator.AppRequest(
		msg.SourceChainID,
		requestID,
		e2e.DefaultTimeout,
		p2p.PrefixMessage(p2p.ProtocolPrefix(acp118.HandlerID), requestBytes),
	)
}

func parseWarpSignatureResponse(m *message.InboundMessage) (*bls.Signature, error) {
	var response *p2ppb.AppResponse
	switch msg := m.Message.(type) {
	case *p2ppb.AppResponse:
		response = msg
	case *p2ppb.AppError:
		return nil, errors.New(msg.ErrorMessage)
	default:
		return nil, fmt.Errorf("unexpected %T message", msg)
	}

	var sigResponse sdk.SignatureResponse
	if err := proto.Unmarshal(response.AppBytes, &sigResponse); err != nil {
		return nil, fmt.Errorf("parsing signature response: %w", err)
	}
	return bls.SignatureFromBytes(sigResponse.Signature)
}
